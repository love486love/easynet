//go:build windows

package netinfo

import (
	"net"
	"testing"
)

func TestParseSSIDChinese(t *testing.T) {
	out := `
接口名称 : WLAN
当前有 1 个接口:

    名称                   : WLAN
    描述                   : MediaTek Wi-Fi 6E MT7922
    GUID                   : 00000000-0000-0000-0000-000000000000
    物理地址               : aa:bb:cc:dd:ee:ff
    状态                   : 已连接
    SSID                   : JXAU-Stu
    BSSID                  : 00:11:22:33:44:55
`
	if got := ParseSSID(out); got != "JXAU-Stu" {
		t.Fatalf("got %q, want JXAU-Stu", got)
	}
}

func TestParseSSIDEnglish(t *testing.T) {
	out := `
    Name                   : WLAN
    State                  : connected
    SSID                   : Campus-WiFi
    BSSID                  : aa:bb:cc:dd:ee:ff
`
	if got := ParseSSID(out); got != "Campus-WiFi" {
		t.Fatalf("got %q, want Campus-WiFi", got)
	}
}

func TestParseSSIDDisconnected(t *testing.T) {
	out := `
    Name                   : WLAN
    State                  : disconnected
`
	if got := ParseSSID(out); got != "" {
		t.Fatalf("未连接时应为空，got %q", got)
	}
}

func TestGetDoesNotPanic(t *testing.T) {
	info, err := Get()
	if err != nil {
		t.Skipf("当前环境无法读取网络信息: %v", err)
	}
	if info.Gateway != "" && net.ParseIP(info.Gateway) == nil {
		t.Fatalf("网关格式非法: %q", info.Gateway)
	}
}
