//go:build windows

// Package creds 使用 Windows DPAPI 保存凭据。
// 密文绑定当前用户与当前机器，换用户或换机器都无法解密。
// 支持多账号：accounts.bin 内保存全部账号与当前选中项。
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
	fileName                = "secret.bin"   // 旧版单账号文件（仅用于迁移）
	storeFileName           = "accounts.bin" // 多账号文件
	filePerm                = 0o600
)

var (
	modCrypt32  = syscall.NewLazyDLL("crypt32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procCryptProtectData   = modCrypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = modCrypt32.NewProc("CryptUnprotectData")
	procLocalFree          = modKernel32.NewProc("LocalFree")
)

// Secret 是单个账号。
type Secret struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Carrier  string `json:"carrier"`
}

// Store 是多账号存储：Accounts 保存全部账号，Current 是守护使用的学号。
type Store struct {
	Accounts []Secret `json:"accounts"`
	Current  string   `json:"current"`
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

// Path 返回旧版单账号密文路径（仅用于迁移）。
func Path(dir string) string { return filepath.Join(dir, fileName) }

// StorePath 返回多账号密文路径。
func StorePath(dir string) string { return filepath.Join(dir, storeFileName) }

// SaveStore 加密并写入多账号文件。
func SaveStore(dir string, s Store) error {
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
	return os.WriteFile(StorePath(dir), enc, filePerm)
}

// LoadStore 读取多账号；首次运行时把旧版 secret.bin 迁移进来。
func LoadStore(dir string) (Store, error) {
	enc, err := os.ReadFile(StorePath(dir))
	if err == nil {
		raw, uerr := unprotect(enc)
		if uerr != nil {
			return Store{}, uerr
		}
		var s Store
		if jerr := json.Unmarshal(raw, &s); jerr != nil {
			return Store{}, fmt.Errorf("解析账号文件失败: %w", jerr)
		}
		return s, nil
	}
	if !os.IsNotExist(err) {
		return Store{}, err
	}

	// 没有多账号文件：尝试迁移旧的单账号
	old, legacyErr := loadLegacy(dir)
	if legacyErr != nil {
		return Store{}, legacyErr
	}
	s := Store{Accounts: []Secret{old}, Current: old.Username}
	if err := SaveStore(dir, s); err != nil {
		return Store{}, err
	}
	_ = os.Remove(Path(dir))
	return s, nil
}

func loadLegacy(dir string) (Secret, error) {
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

// Load 返回当前选中的账号（守护进程使用）。
func Load(dir string) (Secret, error) {
	s, err := LoadStore(dir)
	if err != nil {
		return Secret{}, err
	}
	for _, a := range s.Accounts {
		if a.Username == s.Current {
			return a, nil
		}
	}
	if len(s.Accounts) > 0 {
		return s.Accounts[0], nil
	}
	return Secret{}, errors.New("未保存任何账号")
}

// SaveAccount 新增或更新账号，并把它设为当前账号。
func SaveAccount(dir string, s Secret) (Store, error) {
	store, err := LoadStore(dir)
	if err != nil && !os.IsNotExist(err) {
		return Store{}, err
	}

	replaced := false
	for i := range store.Accounts {
		if store.Accounts[i].Username == s.Username {
			store.Accounts[i] = s
			replaced = true
			break
		}
	}
	if !replaced {
		store.Accounts = append(store.Accounts, s)
	}
	store.Current = s.Username
	if err := SaveStore(dir, store); err != nil {
		return Store{}, err
	}
	return store, nil
}

// SelectAccount 把指定账号设为当前账号。
func SelectAccount(dir, username string) (Store, error) {
	store, err := LoadStore(dir)
	if err != nil {
		return Store{}, err
	}
	for _, a := range store.Accounts {
		if a.Username == username {
			store.Current = username
			if err := SaveStore(dir, store); err != nil {
				return Store{}, err
			}
			return store, nil
		}
	}
	return Store{}, fmt.Errorf("未找到账号 %s", username)
}

// DeleteAccount 删除指定账号；若删的是当前账号，自动切到剩下的第一个。
func DeleteAccount(dir, username string) (Store, error) {
	store, err := LoadStore(dir)
	if err != nil {
		return Store{}, err
	}
	kept := store.Accounts[:0]
	for _, a := range store.Accounts {
		if a.Username != username {
			kept = append(kept, a)
		}
	}
	store.Accounts = kept
	if store.Current == username {
		if len(kept) > 0 {
			store.Current = kept[0].Username
		} else {
			store.Current = ""
		}
	}
	if err := SaveStore(dir, store); err != nil {
		return Store{}, err
	}
	return store, nil
}
