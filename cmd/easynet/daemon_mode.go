package main

import (
	"time"

	"fyne.io/systray"

	"github.com/love486love/easynet/internal/app"
	"github.com/love486love/easynet/internal/config"
	"github.com/love486love/easynet/internal/logx"
	"github.com/love486love/easynet/internal/state"
)

// daemonArg 让同一个 exe 承担两个角色：不带参数是图形界面，带它是后台守护。
// 这样发行包里只有一个 exe，开机自启也指向它，不必再维护第二份二进制。
const daemonArg = "--daemon"

// runDaemonMode 无窗口运行守护进程，并在系统托盘显示状态。
func runDaemonMode() {
	done := make(chan struct{})
	go func() {
		if err := app.RunDaemon(""); err != nil {
			recordError(err)
		}
		close(done)
	}()

	// 守护自身退出（含停止请求、凭据缺失）时，托盘同步消失。
	go func() {
		<-done
		systray.Quit()
	}()

	runTray()

	// 用户从托盘点"退出"：onExit 已写入 stop.flag，这里等守护收尾。
	select {
	case <-done:
	case <-time.After(8 * time.Second):
	}
}

// recordError 在 windowsgui 无控制台的情况下把启动错误写进日志与状态快照。
func recordError(err error) {
	dir, derr := config.UserDir()
	if derr != nil {
		return
	}
	// 界面靠状态快照展示失败原因（例如"读取凭据失败"）。
	_ = state.Write(dir, state.Snapshot{State: "stopped", Message: "启动失败: " + err.Error()})
	logger, closeLog, lerr := logx.New(config.LogDir())
	if lerr != nil {
		return
	}
	defer closeLog()
	logger.Error("守护启动失败", "err", err)
}
