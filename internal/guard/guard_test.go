package guard

import (
	"errors"
	"testing"
)

func baseInput() Input {
	return Input{
		TargetSSIDs:  []string{"JXAU-Stu", "JXAU"},
		CurrentSSID:  "JXAU-Stu",
		PortalHost:   "172.16.121.9",
		AllowedHosts: []string{"172.16.121.9", "10.255.240.8"},
		GatewayMAC:   "aa:bb:cc:dd:ee:ff",
		TrustedMACs:  nil,
	}
}

func TestCheckSecondCampusSSID(t *testing.T) {
	in := baseInput()
	in.CurrentSSID = "JXAU" // 本部校区
	in.PortalHost = "10.255.240.8"
	if _, err := Check(in); err != nil {
		t.Fatalf("本部 SSID + 白名单门户应放行: %v", err)
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

	// 跨校区场景：本部网关是新 MAC。门户已过白名单 → 放行 + 警告，
	// 登录成功后记入信任（若阻断会形成"不登录就记不了"的死锁）。
	in.GatewayMAC = "11:22:33:44:55:66"
	res, err := Check(in)
	if err != nil {
		t.Fatalf("白名单内的门户遇新网关 MAC 应放行: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("新网关 MAC 应给出告警")
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
