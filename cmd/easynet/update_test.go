package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.3.0", "0.3.1", true},
		{"0.3.1", "0.3.0", false},
		{"0.3.0", "0.3.0", false},
		{"v0.2.1", "0.3.0", true},
		{"0.10.0", "0.9.1", false},
		{"1.0.0", "0.9.9", false},
		{"0.4.0-rc1", "0.4.0", false},
	}
	for _, c := range cases {
		if got := versionLess(c.a, c.b); got != c.want {
			t.Errorf("versionLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// 替换失败会留下“目标文件缺失、旧文件在 .old”的坏状态，自愈必须把备份改回来，
// 否则程序再也起不来（0.4.6 自动更新事故的现场就是这样）。
func TestHealUpdateStateRestoresMissingTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "easynet.exe")
	_, old := updatePaths(target)
	if err := os.WriteFile(old, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	if !healUpdateState(nil, target) {
		t.Fatal("目标缺失且备份存在时应执行恢复")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("恢复后目标文件应存在: %v", err)
	}
	if string(got) != "old-binary" {
		t.Fatalf("恢复内容错误: %q", got)
	}
	if fileExists(old) {
		t.Fatal("恢复后旧备份应已被改走")
	}
}

func TestHealUpdateStateKeepsHealthyTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "easynet.exe")
	_, old := updatePaths(target)
	if err := os.WriteFile(target, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("stale"), 0o755); err != nil {
		t.Fatal(err)
	}

	if healUpdateState(nil, target) {
		t.Fatal("目标文件完好时不应触发恢复")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "current" {
		t.Fatalf("目标文件被误改: %q", got)
	}
}

func TestHealUpdateStateNoopWithoutBackup(t *testing.T) {
	target := filepath.Join(t.TempDir(), "easynet.exe")
	if healUpdateState(nil, target) {
		t.Fatal("没有任何文件时不应触发恢复")
	}
	if fileExists(target) {
		t.Fatal("不应无中生有创建目标文件")
	}
}

func TestUpdatePaths(t *testing.T) {
	neu, old := updatePaths(`D:\app\easynet.exe`)
	if neu != `D:\app\.easynet.exe.new` || old != `D:\app\.easynet.exe.old` {
		t.Fatalf("路径拼接错误: %q %q", neu, old)
	}
}
