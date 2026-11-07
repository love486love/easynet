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
	procOpenMutexW   = kernel32.NewProc("OpenMutexW")
	procCloseHandle  = kernel32.NewProc("CloseHandle")
)

// SYNCHRONIZE：打开已存在的互斥体只需要这个权限。
const mutexSynchronize = 0x00100000

// Lock 持有命名互斥体，Release 后自动释放。
type Lock struct {
	handle syscall.Handle
}

// Acquire 尝试获取互斥体。第二个返回值为 false 表示已有实例在运行。
//
// 先用 OpenMutexW 探测：返回 0 就代表互斥体不存在，语义明确。
// 不用 CreateMutexW 的 lastError 判断，因为它在成功创建时不会清零，
// 残留的 183 会被误判成"已存在"（真实踩过的坑）。
func Acquire(name string) (*Lock, bool, error) {
	ptr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, false, err
	}

	existing, _, _ := procOpenMutexW.Call(mutexSynchronize, 0, uintptr(unsafe.Pointer(ptr)))
	if existing != 0 {
		procCloseHandle.Call(existing)
		return nil, false, nil
	}

	h, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(ptr)))
	if h == 0 {
		return nil, false, fmt.Errorf("CreateMutex 失败: %v", callErr)
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
