//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"
)

const appWindowTitle = "epo · 校园网自动认证"

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procMessageBoxW         = user32.NewProc("MessageBoxW")
)

// focusExistingWindow 找到已运行实例的窗口并恢复/置前。
// 新实例启动时旧窗口可能仍在初始化，因此重试一段时间。
func focusExistingWindow() {
	title, _ := syscall.UTF16PtrFromString(appWindowTitle)
	for i := 0; i < 10; i++ {
		hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
		if hwnd != 0 {
			procShowWindow.Call(hwnd, 9) // SW_RESTORE
			procSetForegroundWindow.Call(hwnd)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// showErrorBox 在没有控制台的 GUI 进程里弹出阻塞提示框。
func showErrorBox(title, text string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x10) // MB_ICONERROR
}
