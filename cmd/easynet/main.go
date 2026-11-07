package main

import (
	"embed"
	"os"

	"github.com/love486love/easynet/internal/instance"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 同一个 exe 两个角色：双击是界面，带 --daemon 是无窗口守护（开机自启走这条）。
	if hasDaemonArg() {
		runDaemonMode()
		return
	}
	runGUIMode()
}

func hasDaemonArg() bool {
	for _, arg := range os.Args[1:] {
		if arg == daemonArg {
			return true
		}
	}
	return false
}

func runGUIMode() {
	// 单实例：已有窗口时把它提到前台，不再开第二个。
	lock, ok, err := instance.Acquire(`Local\easynet-gui`)
	if err != nil {
		println("单实例检查失败:", err.Error())
	} else if !ok {
		focusExistingWindow()
		return
	}
	if lock != nil {
		defer lock.Release()
	}

	// WebView2 缺失时给出可操作的提示，而不是静默无反应。
	if !webView2Available() {
		showErrorBox("easynet · 校园网自动认证", "未检测到 Microsoft Edge WebView2 运行时。\n\n请安装 WebView2 Runtime（微软官网免费下载），安装后重新打开本程序。")
		return
	}

	app := NewApp()

	runErr := wails.Run(&options.App{
		Title:            appWindowTitle,
		Width:            1000,
		Height:           680,
		MinWidth:         860,
		MinHeight:        560,
		Frameless:        true,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 20, G: 22, B: 26, A: 1},
		OnStartup:        app.startup,
		Bind:             []interface{}{app},
	})
	if runErr != nil {
		println("启动失败:", runErr.Error())
	}
}
