//go:build windows

// Package creds 使用 Windows DPAPI 保存凭据。
// 密文绑定当前用户与当前机器，换用户或换机器都无法解密。
package creds

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	cryptProtectUIForbidden = 0x1
	fileName                = "secret.bin"
	filePerm                = 0o600
)

var (
	modCrypt32  = syscall.NewLazyDLL("crypt32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procCryptProtectData   = modCrypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = modCrypt32.NewProc("CryptUnprotectData")
	procLocalFree          = modKernel32.NewProc("LocalFree")
)

// Secret 是保存到磁盘的凭据内容。
type Secret struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Carrier  string `json:"carrier"`
}

type dataBlob struct {
	size uint32
	data *byte
}

func protect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("待加密数据为空")
	}
	in := dataBlob{size: uint32(len(data)), data: &data[0]}
	var out dataBlob

	if r, _, _ := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	); r == 0 {
		return nil, errors.New("CryptProtectData 失败")
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.data)))

	buf := unsafe.Slice(out.data, out.size)
	result := make([]byte, len(buf))
	copy(result, buf)
	return result, nil
}

func unprotect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("密文为空")
	}
	in := dataBlob{size: uint32(len(data)), data: &data[0]}
	var out dataBlob

	if r, _, _ := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	); r == 0 {
		return nil, errors.New("CryptUnprotectData 失败（密文可能来自其他用户或机器）")
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.data)))

	buf := unsafe.Slice(out.data, out.size)
	result := make([]byte, len(buf))
	copy(result, buf)
	return result, nil
}

// Path 返回密文文件路径。
func Path(dir string) string {
	return filepath.Join(dir, fileName)
}

// Save 加密并写入凭据文件。
func Save(dir string, s Secret) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	enc, err := protect(raw)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(Path(dir), enc, filePerm)
}

// Load 读取并解密凭据文件。
func Load(dir string) (Secret, error) {
	enc, err := os.ReadFile(Path(dir))
	if err != nil {
		return Secret{}, err
	}
	raw, err := unprotect(enc)
	if err != nil {
		return Secret{}, err
	}
	var s Secret
	if err := json.Unmarshal(raw, &s); err != nil {
		return Secret{}, fmt.Errorf("解析凭据失败: %w", err)
	}
	return s, nil
}

// Delete 删除本地凭据文件；文件不存在时视为成功。
func Delete(dir string) error {
	err := os.Remove(Path(dir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
