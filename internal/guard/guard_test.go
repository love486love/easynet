package guard

import (
	"errors"
	"testing"
)

func baseInput() Input {
	return Input{
		TargetSSID:   "JXAU-Stu",
		CurrentSSID:  "JXAU-Stu",
		PortalHost:   "172.16.121.9",
		AllowedHosts: []string{"172.16.121.9"},
		GatewayMAC:   "aa:bb:cc:dd:ee:ff",
		TrustedMACs:  nil,
	}
}

func TestCheckPass(t *testing.T) {
	if _, err := Check(baseInput()); err != nil {
		t.Fatalf("正常输入不应报错: %v", err)
	}
}

func TestCheckSSIDMismatch(t *testing.T) {
	in := baseInput()
	in.CurrentSSID = "CMCC-Free"
	if _, err := Check(in); !errors.Is(err, ErrSSIDMismatch) {
		t.Fatalf("应返回 ErrSSIDMismatch，实际 %v", err)
	}
}

func TestCheckNoWireless(t *testing.T) {
	in := baseInput()
	in.CurrentSSID = ""
	if _, err := Check(in); err == nil {
		t.Fatal("未连接无线网络应报错")
	}
}

func TestCheckPortalNotAllowed(t *testing.T) {
	in := baseInput()
	in.PortalHost = "192.168.1.1"
	if _, err := Check(in); !errors.Is(err, ErrPortalNotAllowed) {
		t.Fatalf("非白名单主机应被拒绝，实际 %v", err)
	}
}

func TestCheckRandomMACWarning(t *testing.T) {
	in := baseInput()
	in.RandomMAC = true
	res, err := Check(in)
	if err != nil {
		t.Fatalf("随机 MAC 不应阻断登录: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("应给出告警")
	}
}

func TestCheckTrustedGateway(t *testing.T) {
	in := baseInput()
	in.TrustedMACs = []string{"AA-BB-CC-DD-EE-FF"}
	if _, err := Check(in); err != nil {
		t.Fatalf("MAC 大小写/分隔符应视为相等: %v", err)
	}

	in.GatewayMAC = "11:22:33:44:55:66"
	if _, err := Check(in); !errors.Is(err, ErrUntrustedGateway) {
		t.Fatalf("陌生网关应被拒绝，实际 %v", err)
	}
}

func TestCheckEmptyGatewayMAC(t *testing.T) {
	in := baseInput()
	in.GatewayMAC = ""
	in.TrustedMACs = []string{"aa:bb:cc:dd:ee:ff"}
	res, err := Check(in)
	if err != nil {
		t.Fatalf("读不到网关 MAC 时不应阻断: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("应给出告警")
	}
}

func TestCheckSSIDCaseInsensitive(t *testing.T) {
	in := baseInput()
	in.CurrentSSID = "jxau-stu"
	if _, err := Check(in); err != nil {
		t.Fatalf("SSID 比较应忽略大小写: %v", err)
	}
}
