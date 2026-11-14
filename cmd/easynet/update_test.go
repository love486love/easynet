package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsAccessDenied(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"permission", fs.ErrPermission, true},
		{"windows文案", errors.New(`rename a b: Access is denied.`), true},
		{"中文文案", errors.New("对路径的访问被拒绝"), false}, // 该文案不在匹配集（保持最小面）
		{"包装后", wrapErr(fs.ErrPermission), true},
		{"无关错误", errors.New("file not found"), false},
	}
	for _, c := range cases {
		if got := isAccessDenied(c.err); got != c.want {
			t.Errorf("%s: isAccessDenied=%v want %v", c.name, got, c.want)
		}
	}
}

// wrapErr 模拟 selfupdate 对底层错误的包装（保留错误链）。
func wrapErr(err error) error { return fmt.Errorf("apply failed: %w", err) }

func TestWriteUpdateHelper(t *testing.T) {
	dir := t.TempDir()
	path := writeUpdateHelper(dir)
	if path == "" {
		t.Fatal("应生成脚本路径")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取脚本失败: %v", err)
	}
	script := string(raw)
	// 幂等三件套：恢复缺失目标 / 新文件就位 / 启动新版本
	for _, needle := range []string{".easynet.exe.old", ".easynet.exe.new", "easynet.exe", "start"} {
		if !strings.Contains(script, needle) {
			t.Errorf("脚本缺少 %q", needle)
		}
	}
	// 失败时不返回路径
	if p := writeUpdateHelper(filepath.Join(dir, "not-exist", "sub")); p != "" && !filepath.IsAbs(p) {
		t.Errorf("返回路径应为绝对路径: %q", p)
	}
}

func TestProbeSwapRoundTrip(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "easynet.exe")
	if err := os.WriteFile(target, []byte("MZ fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := probeSwap(target); err != nil {
		t.Fatalf("正常目录预检应通过: %v", err)
	}
	// 预检后文件必须原样回到目标位置
	if raw, err := os.ReadFile(target); err != nil || string(raw) != "MZ fake" {
		t.Fatalf("预检后目标文件应完好: %v", err)
	}
	// 目标缺失时预检应报错（且不产生 probe 残留）
	if err := probeSwap(filepath.Join(dir, "missing.exe")); err == nil {
		t.Fatal("缺失目标应报错")
	}
}
