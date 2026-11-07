//go:build windows

package instance

import (
	"fmt"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func uniqueName() string {
	return fmt.Sprintf(`Local\easynet-test-%d`, time.Now().UnixNano())
}

func TestAcquireIsExclusive(t *testing.T) {
	name := uniqueName()

	l1, ok, err := Acquire(name)
	if err != nil || !ok {
		t.Fatalf("首次获取失败: err=%v ok=%v", err, ok)
	}

	l2, ok2, err := Acquire(name)
	if err != nil {
		t.Fatalf("第二次获取报错: %v", err)
	}
	if ok2 {
		l2.Release()
		t.Fatal("同名互斥体被重复获取")
	}

	l1.Release()
	l3, ok3, err := Acquire(name)
	if err != nil || !ok3 {
		t.Fatalf("释放后应能重新获取: err=%v ok=%v", err, ok3)
	}
	l3.Release()
}

// 复现线上问题：线程上一条错误码是 183 时，Acquire 不应把不存在的互斥体误判为"已存在"。
func TestAcquireIgnoresStaleLastError(t *testing.T) {
	// 人为制造一条 183 错误码（CreateMutexW 的 ERROR_ALREADY_EXISTS）。
	_, _, _ = syscall.NewLazyDLL("kernel32.dll").NewProc("SetLastError").Call(183)

	name := uniqueName()
	l, ok, err := Acquire(name)
	if err != nil {
		t.Fatalf("获取失败: %v", err)
	}
	if !ok {
		t.Fatal("互斥体并不存在，却被误判为已有实例在运行")
	}
	l.Release()
}

// 确保 OpenMutexW 预检本身能识别真实存在的互斥体（不依赖错误码）。
func TestOpenMutexSeesExisting(t *testing.T) {
	name := uniqueName()
	ptr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	h, _, _ := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(ptr)))
	if h == 0 {
		t.Fatal("准备互斥体失败")
	}
	defer procCloseHandle.Call(h)

	if l, ok, _ := Acquire(name); ok {
		l.Release()
		t.Fatal("已存在的互斥体被重复获取")
	}
}
