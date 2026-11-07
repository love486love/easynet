// epo-daemon 是无控制台的常驻守护入口（windowsgui 子系统）。
// 开机自启直接指向本程序，启动后即开始守护，无需额外参数。
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/love486love/epo-2026/internal/app"
	"github.com/love486love/epo-2026/internal/config"
	"github.com/love486love/epo-2026/internal/logx"
)

func main() {
	if err := app.RunDaemon(""); err != nil {
		// windowsgui 没有控制台，错误写日志文件，便于用户排查。
		recordError(err)
		os.Exit(1)
	}
}

func recordError(err error) {
	dir, derr := config.UserDir()
	if derr != nil {
		return
	}
	logger, closeLog, lerr := logx.New(filepath.Join(dir, "logs"))
	if lerr != nil {
		return
	}
	defer closeLog()
	logger.Error("守护启动失败", "err", err)
	_ = fmt.Sprint(err)
}
