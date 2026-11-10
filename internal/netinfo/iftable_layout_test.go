//go:build windows

package netinfo

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// TestEntryLayoutMatchesSystem 是 GetIfEntry2 单行字段偏移的硬验证，
// 分两层：
//
//  1. Go 侧独立取默认路由 ifIndex，单行读别名，必须与系统同卡
//     InterfaceAlias 完全一致（@28 偏移错位时读出的是别的字段，撞不对）。
//  2. Counters() 的 In/OutOctets 必须与系统统计（同为 MIB 口径）一致。
//     累计值对照，取样时差造成的变化远小于总量，5% 误差已很宽松。
//
// 改动 iftable_windows.go 的偏移常量后必须跑本测试。
func TestEntryLayoutMatchesSystem(t *testing.T) {
	ps := psNetFacts(t) // 一次拿：默认路由卡的别名、索引、同卡收发字节

	// Go 侧独立走 defaultRoute，不复用 PS 的 idx
	_, idx, err := defaultRoute()
	if err != nil {
		t.Fatalf("defaultRoute: %v", err)
	}
	if idx == 0 {
		t.Fatal("defaultRoute 返回 ifIndex=0")
	}

	// 1) 别名对照
	alias := IfAlias(idx)
	if alias == "" {
		t.Fatalf("IfAlias(%d) 为空", idx)
	}
	if alias != ps.alias {
		t.Errorf("别名不一致: go=%q ps=%q（接口别名 @28 偏移疑似错位）", alias, ps.alias)
	}

	// 2) 计数对照同卡系统真值
	in, out, err := Counters()
	if err != nil {
		t.Fatalf("Counters: %v", err)
	}
	if in == 0 && out == 0 {
		t.Fatal("计数全为 0，偏移或取卡逻辑有误")
	}
	if diff := relDiff(in, ps.in); diff > 0.05 {
		t.Errorf("InOctets 与系统真值不符: go=%d ps=%d 相差 %.1f%%，InOctets@%d 疑似错位", in, ps.in, diff*100, offInOctets)
	}
	if diff := relDiff(out, ps.out); diff > 0.05 {
		t.Errorf("OutOctets 与系统真值不符: go=%d ps=%d 相差 %.1f%%，OutOctets@%d 疑似错位", out, ps.out, diff*100, offOutOctets)
	}
	t.Logf("布局验证通过: idx=%d alias=%q in=%d out=%d (ps in=%d out=%d)",
		idx, alias, in, out, ps.in, ps.out)
}

type netFacts struct {
	alias string // 默认路由卡的接口别名（系统真值）
	idx   uint32 // 默认路由接口索引
	in    uint64 // 该卡累计收（系统真值，MIB 口径）
	out   uint64 // 该卡累计发
}

func psNetFacts(t *testing.T) netFacts {
	t.Helper()
	script := `
$idx = (Get-NetRoute -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue |
    Where-Object AddressFamily -eq 'IPv4' |
    Sort-Object RouteMetric | Select-Object -First 1).InterfaceIndex
$alias = ''; $rxb = [int64]0; $txb = [int64]0
if ($idx) {
  $a = Get-NetAdapter -InterfaceIndex $idx -ErrorAction SilentlyContinue
  if ($a) { $alias = $a.InterfaceAlias }
  $st = $a | Get-NetAdapterStatistics -ErrorAction SilentlyContinue
  if ($st) { $rxb = [int64]$st.ReceivedBytes; $txb = [int64]$st.SentBytes }
}
$alias
$idx
$rxb
$txb
`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	outBytes, err := cmd.Output()
	if err != nil {
		t.Fatalf("取系统网络信息失败: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(outBytes)), "\n")
	if len(lines) < 4 {
		t.Fatalf("系统信息输出不完整: %q", string(outBytes))
	}
	idx64, _ := strconv.ParseUint(strings.TrimSpace(lines[1]), 10, 32)
	in, _ := strconv.ParseUint(strings.TrimSpace(lines[2]), 10, 64)
	outv, _ := strconv.ParseUint(strings.TrimSpace(lines[3]), 10, 64)
	return netFacts{
		alias: strings.TrimRight(strings.TrimSpace(lines[0]), "\r"),
		idx:   uint32(idx64),
		in:    in,
		out:   outv,
	}
}

func relDiff(a, b uint64) float64 {
	if a == b {
		return 0
	}
	var d uint64
	if a > b {
		d = a - b
	} else {
		d = b - a
	}
	base := float64(b)
	if base == 0 {
		return 1
	}
	return float64(d) / base
}
