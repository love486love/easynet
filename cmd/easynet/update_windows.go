//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/love486love/easynet/internal/config"
	"github.com/love486love/easynet/internal/state"
)

// uninstallKey 是 Inno Setup 写入的卸载项 AppId，用来判断是否走的安装版。
const uninstallKey = `{6D3E9B52-8C1F-4A7D-9E44-2B7A5F0C1D88}_is1`

func installedMode() bool {
	for _, root := range []string{"HKCU", "HKLM"} {
		key := root + `\Software\Microsoft\Windows\CurrentVersion\Uninstall\` + uninstallKey
		cmd := exec.Command("reg", "query", key)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Run(); err == nil {
			return true
		}
	}
	return false
}

// applyInstaller 适用于安装版：下载新版安装包并静默运行。
func applyInstaller(info UpdateInfo) (string, error) {
	if info.SetupURL == "" {
		return "", fmt.Errorf("安装版更新需要新版安装包，请到 Release 页面手动下载")
	}
	stopDaemonForUpdate()

	data, err := downloadAsset(info.SetupURL)
	if err != nil {
		return "", fmt.Errorf("下载安装包失败：%v", err)
	}
	path := filepath.Join(os.TempDir(), "easynet-setup.exe")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		return "", fmt.Errorf("保存安装包失败：%v", err)
	}

	cmd := exec.Command(path, "/SILENT", "/CLOSEAPPLICATIONS", "/NORESTART")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("启动安装程序失败：%v", err)
	}
	time.AfterFunc(300*time.Millisecond, func() { os.Exit(0) })
	return "正在安装新版本，安装完成后会自动打开", nil
}

// stopDaemonForUpdate 让守护先退出，避免新旧版本同时占用 exe。
func stopDaemonForUpdate() {
	if dir, err := config.UserDir(); err == nil {
		_ = state.RequestStop(dir)
	}
	time.Sleep(1200 * time.Millisecond)
}

// launchAfterExit 延迟启动新版本，确保当前进程先释放单实例互斥体。
func launchAfterExit() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	line := fmt.Sprintf(`ping -n 3 127.0.0.1 >nul & start "" "%s"`, exe)
	cmd := exec.Command("cmd", "/c", line)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd.Start()
	time.AfterFunc(300*time.Millisecond, func() { os.Exit(0) })
}
