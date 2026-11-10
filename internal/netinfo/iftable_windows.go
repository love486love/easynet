//go:build windows

package netinfo

import (
	"encoding/binary"
	"fmt"
	"unsafe"
)

// 读取网卡流量计数（GetIfEntry2 单行），供实时网速显示。
//
// MIB_IF_ROW2 的字节布局无法可靠手写：位域段的 MSVC 分配规则复杂，
// 实测按 netioapi.h 镜像的表方案行 stride 与真实不符，第 2 行起整行
// 错位。改用 GetIfEntry2 单行查询：预填 InterfaceIndex 到 4096 字节
// 大缓冲，API 只按其真实 sizeof 回填，缓冲偏大无害，彻底规避行大小
// 问题。
//
// 字段偏移由 TestEntryLayoutMatchesSystem 与系统真值
// （Get-NetAdapterStatistics）区间夹逼实证（计数器单调递增，
// 真值必夹在两次调用读到的值之间），改动偏移常量必须跑该测试。
//
// 已实证偏移（Windows x64）：
//
//	InterfaceIndex @8   Alias(WCHAR[]) @28
//	InOctets @1208      OutOctets @1280    真实行大小 ∈ (1337, 4096]
var procGetIfEntry2 = modIphlpapi.NewProc("GetIfEntry2")

const (
	ifEntryBufSize = 4096
	offIfIndex     = 8
	offAlias       = 28
	aliasBytes     = 514 // (256+1)*2，WCHAR 别名区
	offInOctets    = 1208
	offOutOctets   = 1280
)

// getIfEntry 返回 ifIndex 单行的原始字节。
func getIfEntry(ifIndex uint32) ([]byte, error) {
	if ifIndex == 0 {
		return nil, fmt.Errorf("ifIndex 为 0")
	}
	row := make([]byte, ifEntryBufSize)
	binary.LittleEndian.PutUint32(row[offIfIndex:], ifIndex)
	r, _, _ := procGetIfEntry2.Call(uintptr(unsafe.Pointer(&row[0])))
	if r != 0 {
		return nil, fmt.Errorf("GetIfEntry2(%d) 失败: %d", ifIndex, r)
	}
	return row, nil
}

// Counters 返回默认路由网卡的累计收发字节。无默认路由或查询失败时返回错误。
func Counters() (in, out uint64, err error) {
	_, ifIndex, err := defaultRoute()
	if err != nil {
		return 0, 0, err
	}
	row, err := getIfEntry(ifIndex)
	if err != nil {
		return 0, 0, err
	}
	in = binary.LittleEndian.Uint64(row[offInOctets:])
	out = binary.LittleEndian.Uint64(row[offOutOctets:])
	return in, out, nil
}

// IfAlias 返回 ifIndex 网卡的接口别名（WCHAR 串，遇 NUL 截断）。
func IfAlias(ifIndex uint32) string {
	row, err := getIfEntry(ifIndex)
	if err != nil {
		return ""
	}
	b := row[offAlias : offAlias+aliasBytes]
	words := make([]uint16, len(b)/2)
	for i := range words {
		words[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	return utf16z(words)
}

func utf16z(b []uint16) string {
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(utf16Decode(b[:n]))
}

func utf16Decode(b []uint16) []rune {
	runes := make([]rune, 0, len(b))
	for i := 0; i < len(b); i++ {
		r := rune(b[i])
		// 拼接代理对
		if r >= 0xD800 && r <= 0xDBFF && i+1 < len(b) {
			if lo := rune(b[i+1]); lo >= 0xDC00 && lo <= 0xDFFF {
				r = (r-0xD800)<<10 | (lo - 0xDC00) + 0x10000
				i++
			}
		}
		runes = append(runes, r)
	}
	return runes
}
