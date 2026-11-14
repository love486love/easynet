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

// Secret 是单个账号。Campus 标记所属校区（dongqu/benbu），
// 旧数据没有该字段，按东区处理（0.5.0 之前只支持东区）。
type Secret struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Carrier  string `json:"carrier"`
	Campus   string `json:"campus,omitempty"`
}

// 校区标识，与 config.Campus* 同值（避免 creds 反向依赖 config）。
const (
	CampusDongqu = "dongqu"
	CampusBenbu  = "benbu"
)

// CampusID 返回账号所属校区，空值按东区处理。
func (s Secret) CampusID() string {
	if s.Campus == "" {
		return CampusDongqu
	}
	return s.Campus
}

// Store 是多账号存储：Accounts 保存全部账号；
// Currents 按校区记录守护登录时使用的账号（东区/本部各一个）。
// Current 是旧版单当前字段，保留读取兼容（新写入时同步指向东区当前）。
type Store struct {
	Accounts []Secret          `json:"accounts"`
	Current  string            `json:"current"`
	Currents map[string]string `json:"currents,omitempty"`
}

// normalize 补齐旧数据：账号校区空值按东区、缺失 currents 由 legacy Current 推导。
func (s *Store) normalize() {
	for i := range s.Accounts {
		if s.Accounts[i].Campus == "" {
			s.Accounts[i].Campus = CampusDongqu
		}
	}
	if s.Currents == nil {
		s.Currents = map[string]string{}
	}
	if len(s.Currents) == 0 && s.Current != "" {
		for _, a := range s.Accounts {
			if a.Username == s.Current {
				s.Currents[a.CampusID()] = a.Username
				break
			}
		}
	}
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
		s.normalize()
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
	s.normalize()
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

// Load 返回东区当前账号（启动检查等兼容场景使用）。
func Load(dir string) (Secret, error) {
	s, err := LoadStore(dir)
	if err != nil {
		return Secret{}, err
	}
	if u := s.Currents[CampusDongqu]; u != "" {
		for _, a := range s.Accounts {
			if a.Username == u {
				return a, nil
			}
		}
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

// LoadForCampus 返回指定校区当前守护使用的账号。
// 该校区没有账号时返回明确错误，由登录侧提示用户先在界面添加。
func LoadForCampus(dir, campus string) (Secret, error) {
	s, err := LoadStore(dir)
	if err != nil {
		return Secret{}, err
	}
	if u := s.Currents[campus]; u != "" {
		for _, a := range s.Accounts {
			if a.Username == u && a.CampusID() == campus {
				return a, nil
			}
		}
	}
	for _, a := range s.Accounts {
		if a.CampusID() == campus {
			return a, nil
		}
	}
	name := "本部"
	if campus == CampusDongqu {
		name = "东区"
	}
	return Secret{}, fmt.Errorf("%s校区尚未保存账号", name)
}

// SaveAccount 新增或更新账号，并把它设为所属校区的当前账号。
func SaveAccount(dir string, s Secret) (Store, error) {
	if s.Campus == "" {
		s.Campus = CampusDongqu
	}
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
	if store.Currents == nil {
		store.Currents = map[string]string{}
	}
	store.Currents[s.CampusID()] = s.Username
	store.Current = s.Username
	if err := SaveStore(dir, store); err != nil {
		return Store{}, err
	}
	return store, nil
}

// SelectAccount 把指定账号设为所属校区的当前账号。
func SelectAccount(dir, username string) (Store, error) {
	store, err := LoadStore(dir)
	if err != nil {
		return Store{}, err
	}
	for _, a := range store.Accounts {
		if a.Username == username {
			if store.Currents == nil {
				store.Currents = map[string]string{}
			}
			store.Currents[a.CampusID()] = username
			store.Current = username
			if err := SaveStore(dir, store); err != nil {
				return Store{}, err
			}
			return store, nil
		}
	}
	return Store{}, fmt.Errorf("未找到账号 %s", username)
}

// DeleteAccount 删除指定账号；若删的是某校区当前账号，
// 该校区自动切到剩余的第一个同校区账号（没有则留空）。
func DeleteAccount(dir, username string) (Store, error) {
	store, err := LoadStore(dir)
	if err != nil {
		return Store{}, err
	}
	kept := store.Accounts[:0]
	deletedCampus := ""
	for _, a := range store.Accounts {
		if a.Username != username {
			kept = append(kept, a)
		} else {
			deletedCampus = a.CampusID()
		}
	}
	store.Accounts = kept
	if deletedCampus != "" && store.Currents[deletedCampus] == username {
		store.Currents[deletedCampus] = ""
		for _, a := range kept {
			if a.CampusID() == deletedCampus {
				store.Currents[deletedCampus] = a.Username
				break
			}
		}
	}
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
