//go:build windows

package netinfo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Connect 让系统连接指定 SSID。
//
// netsh 在失败（例如 profile 不存在）时退出码通常仍是 0，所以不能只看 err，
// 必须轮询「当前 SSID 是否真的变成目标网络」。确认没连上后，再补建一份
// 开放网络的 profile 并重试——覆盖用户刚在系统里「忘记」该网络的场景。
func Connect(ssid string) error {
	if ssid == "" {
		return fmt.Errorf("SSID 不能为空")
	}

	if cur, err := currentSSID(); err == nil && cur == ssid {
		return nil
	}

	_, _ = runNetsh("wlan", "connect", "name="+ssid, "ssid="+ssid)
	if waitConnected(ssid, 6*time.Second) {
		return nil
	}

	if err := addProfile(ssid); err != nil {
		return err
	}
	_, _ = runNetsh("wlan", "connect", "name="+ssid, "ssid="+ssid)
	if waitConnected(ssid, 8*time.Second) {
		return nil
	}

	return fmt.Errorf("连接 %s 超时，请确认该网络在信号范围内且无线网卡已开启", ssid)
}

func waitConnected(ssid string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cur, err := currentSSID(); err == nil && cur == ssid {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func runNetsh(args ...string) (string, error) {
	cmd := exec.Command("netsh", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// openProfileTemplate 是开放网络（无加密）的 WLAN profile。
const openProfileTemplate = `<?xml version="1.0"?>
<WLANProfile xmlns="http://www.microsoft.com/networking/WLAN/profile/v1">
	<name>%s</name>
	<SSIDConfig><SSID><name>%s</name></SSID></SSIDConfig>
	<connectionType>ESS</connectionType>
	<connectionMode>auto</connectionMode>
	<MSM><security>
		<authEncryption>
			<authentication>open</authentication>
			<encryption>none</encryption>
			<useOneX>false</useOneX>
		</authEncryption>
	</security></MSM>
</WLANProfile>`

func addProfile(ssid string) error {
	dir := filepath.Join(os.TempDir(), "easynet")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "wlan-profile.xml")
	name := escapeXML(ssid)
	xml := fmt.Sprintf(openProfileTemplate, name, name)
	if err := os.WriteFile(path, []byte(xml), 0o644); err != nil {
		return err
	}
	defer os.Remove(path)

	// filename 参数需要带引号，否则含空格或特殊字符的路径会被截断。
	if out, err := runNetsh("wlan", "add", "profile", `filename="`+path+`"`, "user=current"); err != nil {
		return fmt.Errorf("创建 %s 的网络配置失败: %v（%s）", ssid, err, brief(out))
	}
	// netsh 失败时也会返回 0，这里用「profile 是否出现在列表里」兜底判断。
	if out, _ := runNetsh("wlan", "show", "profile", "name="+ssid); !strings.Contains(out, ssid) {
		return fmt.Errorf("创建 %s 的网络配置失败: %s", ssid, brief(out))
	}
	return nil
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// brief 取 netsh 输出的最后一行，避免把整段帮助文本塞进日志。
func brief(out string) string {
	s := strings.TrimSpace(out)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = strings.TrimSpace(s[i+1:])
	}
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}
