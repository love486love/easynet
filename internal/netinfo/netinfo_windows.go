//go:build windows

// Package netinfo 读取当前 WLAN 状态。
//
// SSID 通过 netsh 读取：输出里的 "SSID" 键本身不随系统语言变化，
// 用键名解析比手算 WlanAPI 结构体偏移更稳（后者一旦字段对不齐会直接崩溃）。
// 网关与网卡信息取自 IP 路由表与 net 包。
package netinfo

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

// Info 是判定与校验所需的本机网络信息。
type Info struct {
	SSID       string
	IPv4       string
	Gateway    string
	GatewayMAC string // 默认网关的链路层地址，来自 ARP
	MAC        string
	RandomMAC  bool // 网卡 MAC 是否置了本地管理位（随机硬件地址）
}

var (
	modIphlpapi           = syscall.NewLazyDLL("iphlpapi.dll")
	procGetIpForwardTable = modIphlpapi.NewProc("GetIpForwardTable")
	procSendARP           = modIphlpapi.NewProc("SendARP")
)

const errorInsufficientBuffer = 122

// Get 返回当前 WLAN 状态。未连接无线网络时 SSID 为空。
func Get() (Info, error) {
	var info Info

	ssid, err := currentSSID()
	if err != nil {
		return info, err
	}
	info.SSID = ssid

	gw, ifIndex, err := defaultRoute()
	if err != nil {
		return info, err
	}
	info.Gateway = gw
	if gw != "" {
		info.GatewayMAC = arpMAC(net.ParseIP(gw))
	}
	if ifIndex == 0 {
		return info, nil
	}

	iface, err := net.InterfaceByIndex(int(ifIndex))
	if err != nil {
		return info, fmt.Errorf("按索引取网卡失败: %w", err)
	}
	info.MAC = iface.HardwareAddr.String()
	if hw := iface.HardwareAddr; len(hw) > 0 {
		info.RandomMAC = hw[0]&0x02 != 0
	}

	for _, a := range mustAddrs(iface) {
		if ipn, ok := a.(*net.IPNet); ok {
			if v4 := ipn.IP.To4(); v4 != nil {
				info.IPv4 = v4.String()
				break
			}
		}
	}
	return info, nil
}

func mustAddrs(iface *net.Interface) []net.Addr {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}
	return addrs
}

func currentSSID() (string, error) {
	out, err := exec.Command("netsh", "wlan", "show", "interfaces").Output()
	if err != nil {
		return "", fmt.Errorf("执行 netsh 失败: %w", err)
	}
	return ParseSSID(string(out)), nil
}

// ParseSSID 从 netsh 输出中提取 SSID。
// 跳过 BSSID 行；键名 "SSID" 在中英文系统下均不变。
func ParseSSID(out string) string {
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		upper := strings.ToUpper(t)
		if !strings.HasPrefix(upper, "SSID") || strings.HasPrefix(upper, "BSSID") {
			continue
		}
		if i := strings.Index(t, ":"); i >= 0 {
			return strings.TrimSpace(t[i+1:])
		}
	}
	return ""
}

// ipv4FromMIB 把 MIB 路由表里的网络序 4 字节还原为点分十进制。
// 小端机器把 10.100.0.1 读成 0x0100640A，因此按低字节到高字节取。
func ipv4FromMIB(v uint32) string {
	return net.IPv4(byte(v), byte(v>>8), byte(v>>16), byte(v>>24)).String()
}

// arpMAC 通过 SendARP 取同网段主机（默认网关）的链路层地址。
// 失败时返回空串：调用方应降级为"仅告警"，不能因此阻断登录。
func arpMAC(ip net.IP) string {
	v4 := ip.To4()
	if v4 == nil {
		return ""
	}
	// SendARP 接受与 inet_addr 相同的 4 字节网络序值；
	// 在小端机器上等价于直接按字节读成 uint32。
	dest := uint32(v4[0]) | uint32(v4[1])<<8 | uint32(v4[2])<<16 | uint32(v4[3])<<24

	var mac [8]byte
	length := uint32(6)
	r, _, _ := procSendARP.Call(
		uintptr(dest), 0,
		uintptr(unsafe.Pointer(&mac[0])),
		uintptr(unsafe.Pointer(&length)),
	)
	if r != 0 || length < 6 {
		return ""
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
}

// defaultRoute 从 IP 路由表取默认网关及其接口索引。
func defaultRoute() (gateway string, ifIndex uint32, err error) {
	var size uint32
	r, _, _ := procGetIpForwardTable.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if r != errorInsufficientBuffer {
		return "", 0, fmt.Errorf("GetIpForwardTable 预分配失败: %d", r)
	}

	buf := make([]byte, size)
	ptr := unsafe.Pointer(&buf[0])
	r, _, _ = procGetIpForwardTable.Call(uintptr(ptr), uintptr(unsafe.Pointer(&size)), 0)
	if r != 0 {
		return "", 0, fmt.Errorf("GetIpForwardTable 失败: %d", r)
	}

	count := *(*uint32)(ptr)
	const rowSize = 14 * 4 // MIB_IPFORWARDROW 共 14 个 DWORD
	base := unsafe.Add(ptr, 4)

	bestMetric := uint32(0xFFFFFFFF)
	for i := uint32(0); i < count; i++ {
		row := unsafe.Add(base, uintptr(i)*rowSize)
		dest := *(*uint32)(row)
		mask := *(*uint32)(unsafe.Add(row, 4))
		if dest != 0 || mask != 0 {
			continue
		}
		nextHop := *(*uint32)(unsafe.Add(row, 12))
		idx := *(*uint32)(unsafe.Add(row, 16))
		metric := *(*uint32)(unsafe.Add(row, 36))
		if metric < bestMetric {
			bestMetric = metric
			gateway = ipv4FromMIB(nextHop)
			ifIndex = idx
		}
	}
	if bestMetric == 0xFFFFFFFF {
		return "", 0, nil
	}
	return gateway, ifIndex, nil
}
