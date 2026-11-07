//go:build windows

// Package autostart 通过 HKCU\Run 实现开机自启。
// 选注册表而非计划任务：无管理员权限、无路径空格/中文的 /TR 截断问题；
// 配合 windowsgui 子系统的 epo-daemon.exe 可做到完全无窗口。
package autostart

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	runKey    = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	valueName = "epo-daemon"
)

// Enable 写入自启项，值为带引号的 exe 路径。
func Enable(exe string) error {
	cmd := exec.Command("reg", "add", runKey, "/v", valueName, "/t", "REG_SZ", "/d", `"`+exe+`"`, "/f")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("写入自启失败: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Disable 删除自启项；本来不存在时视为成功。
func Disable() error {
	cmd := exec.Command("reg", "delete", runKey, "/v", valueName, "/f")
	if out, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "unable to find") || strings.Contains(string(out), "找不到") {
			return nil
		}
		return fmt.Errorf("删除自启失败: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Value 返回当前注册的自启命令；未设置时返回空串。
func Value() (string, error) {
	out, err := exec.Command("reg", "query", runKey, "/v", valueName).Output()
	if err != nil {
		return "", nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, valueName) {
			continue
		}
		if i := strings.Index(line, "REG_SZ"); i >= 0 {
			return strings.TrimSpace(line[i+len("REG_SZ"):]), nil
		}
	}
	return "", nil
}
