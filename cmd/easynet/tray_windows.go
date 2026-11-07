//go:build windows

package main

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"fyne.io/systray"

	"github.com/love486love/easynet/internal/config"
	"github.com/love486love/easynet/internal/state"
)

const wmClose = 0x0010

//go:embed icons/tray.ico
var trayIcon []byte

// runTray 阻塞运行 Windows 托盘消息循环，直到 systray.Quit 被调用。
func runTray() {
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(trayIcon)
	systray.SetTitle("easynet")
	systray.SetTooltip("校园网自动认证守护")

	mStatus := systray.AddMenuItem("检测中…", "守护状态")
	mStatus.Disable()
	systray.AddSeparator()
	mOpen := systray.AddMenuItem("打开界面", "打开 easynet 图形界面")
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

// onExit 在托盘退出时请求守护停止，并把已打开的界面窗口一起关掉；
// 守护会在下一轮循环检查后收尾。
func onExit() {
	if dir, err := config.UserDir(); err == nil {
		_ = state.RequestStop(dir)
	}
	closeGUIWindow()
}

// closeGUIWindow 用窗口标题找到主窗口并发送 WM_CLOSE。
// 界面没打开时 FindWindow 返回 0，直接忽略。
func closeGUIWindow() {
	title, _ := syscall.UTF16PtrFromString(appWindowTitle)
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd == 0 {
		return
	}
	procPostMessageW.Call(hwnd, wmClose, 0, 0)
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

// openGUI 拉起本 exe 的界面模式。已有窗口时新实例会自动退出并置前旧窗口。
func openGUI() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return
	}
}
