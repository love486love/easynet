//go:build windows

package main

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"fyne.io/systray"
	"github.com/love486love/epo-2026/internal/config"
	"github.com/love486love/epo-2026/internal/state"
)

//go:embed icons/tray.ico
var trayIcon []byte

// runTray 阻塞运行 Windows 托盘消息循环，直到 systray.Quit 被调用。
func runTray() {
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(trayIcon)
	systray.SetTitle("epo")
	systray.SetTooltip("校园网自动认证守护")

	mStatus := systray.AddMenuItem("检测中…", "守护状态")
	mStatus.Disable()
	systray.AddSeparator()
	mOpen := systray.AddMenuItem("打开界面", "打开 epo 图形界面")
	mQuit := systray.AddMenuItem("退出", "停止守护并退出")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				openGUI()
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()

	go refreshStatus(mStatus)
}

// onExit 在托盘退出时请求守护停止；守护会在下一轮循环检查后收尾。
func onExit() {
	if dir, err := config.UserDir(); err == nil {
		_ = state.RequestStop(dir)
	}
}

// refreshStatus 每 2 秒把守护快照映射为菜单里的一行文字。
func refreshStatus(item *systray.MenuItem) {
	for {
		if dir, err := config.UserDir(); err == nil {
			snap, rerr := state.Read(dir)
			switch {
			case rerr != nil:
				item.SetTitle("守护未启动")
			case snap.State == "online":
				item.SetTitle("在线 · 守护中")
			case snap.State == "offline":
				item.SetTitle("未认证 · 守护中")
			case snap.State == "stopped":
				item.SetTitle("守护已停止")
			default:
				item.SetTitle("守护中 · " + snap.State)
			}
		}
		time.Sleep(2 * time.Second)
	}
}

// openGUI 拉起同目录下的 epo-gui.exe；不存在时静默忽略。
func openGUI() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	path := filepath.Join(filepath.Dir(exe), "epo-gui.exe")
	if _, err := os.Stat(path); err != nil {
		return
	}
	_ = exec.Command(path).Start()
}
