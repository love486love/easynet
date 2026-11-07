//go:build windows

// Package instance 用命名互斥体保证同一用户只有一个守护进程。
package instance

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
	procCloseHandle  = kernel32.NewProc("CloseHandle")
)

const errorAlreadyExists = 183

// Lock 持有命名互斥体，Release 后自动释放。
type Lock struct {
	handle syscall.Handle
}

// Acquire 尝试获取互斥体。第二个返回值为 false 表示已有实例在运行。
func Acquire(name string) (*Lock, bool, error) {
	ptr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, false, err
	}

	h, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(ptr)))
	if h == 0 {
		return nil, false, fmt.Errorf("CreateMutex 失败: %v", callErr)
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno == errorAlreadyExists {
		procCloseHandle.Call(h)
		return nil, false, nil
	}
	return &Lock{handle: syscall.Handle(h)}, true, nil
}

// Release 关闭互斥体句柄。
func (l *Lock) Release() {
	if l != nil && l.handle != 0 {
		procCloseHandle.Call(uintptr(l.handle))
		l.handle = 0
	}
}
