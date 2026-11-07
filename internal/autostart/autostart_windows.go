//go:build windows

// Package autostart 通过 HKCU\Run 实现开机自启。
// 选注册表而非计划任务：无管理员权限、无路径空格/中文的 /TR 截断问题；
// 配合 windowsgui 子系统与 --daemon 参数可做到完全无窗口。
package autostart

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// 隐藏子进程的控制台窗口：GUI（windowsgui 子系统）调用 reg.exe 时不应闪黑框。
func hidden(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

const (
	runKey    = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	valueName = "easynet"
)

// Enable 写入自启项，值为带引号的 exe 路径，可附加参数（例如 --daemon）。
func Enable(exe string, args ...string) error {
	value := `"` + exe + `"`
	for _, arg := range args {
		value += " " + arg
	}
	cmd := hidden("reg", "add", runKey, "/v", valueName, "/t", "REG_SZ", "/d", value, "/f")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("写入自启失败: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Disable 删除自启项；本来不存在时视为成功。
func Disable() error {
	cmd := hidden("reg", "delete", runKey, "/v", valueName, "/f")
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
	cmd := hidden("reg", "query", runKey, "/v", valueName)
	out, err := cmd.Output()
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
