// epo-daemon 是无控制台的常驻守护入口（windowsgui 子系统）。
// 开机自启直接指向本程序：启动后即开始守护，并在系统托盘显示状态。
package main

import (
	"time"

	"fyne.io/systray"

	"github.com/love486love/epo-2026/internal/app"
	"github.com/love486love/epo-2026/internal/config"
	"github.com/love486love/epo-2026/internal/logx"
	"github.com/love486love/epo-2026/internal/state"
)

func main() {
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

// recordError 在 windowsgui 无控制台的情况下把启动错误写进日志。
func recordError(err error) {
	dir, derr := config.UserDir()
	if derr != nil {
		return
	}
	// 写进状态快照，界面能直接看到失败原因（例如"读取凭据失败"）。
	_ = state.Write(dir, state.Snapshot{State: "stopped", Message: "启动失败: " + err.Error()})
	logger, closeLog, lerr := logx.New(config.LogDir())
	if lerr != nil {
		return
	}
	defer closeLog()
	logger.Error("守护启动失败", "err", err)
}
