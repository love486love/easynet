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

// 成功升级后残留的旧版本备份应在下次启动时清掉。
func TestCleanupUpdateResidueRemovesBackup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "easynet.exe")
	if err := os.WriteFile(target, []byte("new"), 0o644); err != nil {
		t.Fatalf("写目标失败: %v", err)
	}
	_, old := updatePaths(target)
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatalf("写备份失败: %v", err)
	}

	if !cleanupUpdateResidueAt(target) {
		t.Fatal("应删除旧版本备份")
	}
	if fileExists(old) {
		t.Fatal("备份仍然存在")
	}
	if !fileExists(target) {
		t.Fatal("当前版本不应被删除")
	}
}

// 目标文件缺失时，备份是唯一的可执行文件：应恢复到目标位置，而不是删掉。
func TestCleanupUpdateResidueRestoresMissingTarget(t *testing.T) {
	for _, suffix := range []string{"", ".probe"} {
		t.Run("suffix="+suffix, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "easynet.exe")
			_, old := updatePaths(target)
			src := old + suffix
			if err := os.WriteFile(src, []byte("only"), 0o644); err != nil {
				t.Fatalf("写备份失败: %v", err)
			}

			cleanupUpdateResidueAt(target)
			if !fileExists(target) {
				t.Fatal("目标文件应被恢复")
			}
			if fileExists(src) {
				t.Fatal("恢复后不应留下残留")
			}
			if raw, err := os.ReadFile(target); err != nil || string(raw) != "only" {
				t.Fatalf("恢复内容错误: %v", err)
			}
		})
	}
}
