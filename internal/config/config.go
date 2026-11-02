// Package config 负责 profile 的加载与校验。
// profile 中不得包含任何凭据，凭据单独存储。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Profile 是一份学校/校区配置。
type Profile struct {
	SchemaVersion int           `json:"schema_version"`
	ID            string        `json:"id"`
	DisplayName   string        `json:"display_name"`
	Portal        PortalConfig  `json:"portal"`
	Network       NetworkConfig `json:"network"`
	Login         LoginConfig   `json:"login"`
	Policy        PolicyConfig  `json:"policy"`
}

type PortalConfig struct {
	Type      string `json:"type"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	LoginPath string `json:"login_path"`
}

type NetworkConfig struct {
	SSID         string   `json:"ssid"`
	PrimaryProbe string   `json:"primary_probe"`
	ProbeHosts   []string `json:"probe_hosts"`
	BackupProbe  string   `json:"backup_probe"`
}

type LoginConfig struct {
	Carrier     string            `json:"carrier"`
	ExtraParams map[string]string `json:"extra_params"`
}

type PolicyConfig struct {
	OnlineIntervalSec  int  `json:"online_interval_sec"`
	OfflineIntervalSec int  `json:"offline_interval_sec"`
	BackoffMaxSec      int  `json:"backoff_max_sec"`
	MaxLoginsPer10Min  int  `json:"max_logins_per_10min"`
	ShortSessionSec    int  `json:"short_session_sec"`
	RetryOnAuthFail    bool `json:"retry_on_authfail"`
}

// Default 返回江西农业大学学生网的默认配置。
// 与 docs/TECH_DESIGN.md §7 保持一致，并作为二进制内置 fallback。
func Default() Profile {
	return Profile{
		SchemaVersion: 1,
		ID:            "jxau-stu",
		DisplayName:   "江西农业大学学生网",
		Portal: PortalConfig{
			Type:      "drcom-eportal",
			Host:      "172.16.121.9",
			Port:      801,
			LoginPath: "/eportal/portal/login",
		},
		Network: NetworkConfig{
			SSID:         "JXAU-Stu",
			PrimaryProbe: "http://www.jxau.edu.cn/",
			ProbeHosts:   []string{"www.jxau.edu.cn", "jxau.edu.cn"},
			BackupProbe:  "http://www.baidu.com/",
		},
		Login: LoginConfig{
			Carrier: "cmcc",
			ExtraParams: map[string]string{
				"jsVersion":     "4.2.1",
				"terminal_type": "1",
				"login_method":  "1",
				"lang":          "zh-cn",
			},
		},
		Policy: PolicyConfig{
			OnlineIntervalSec:  120,
			OfflineIntervalSec: 60,
			BackoffMaxSec:      60,
			MaxLoginsPer10Min:  3,
			ShortSessionSec:    60,
			RetryOnAuthFail:    false,
		},
	}
}

// UserDir 返回 %LOCALAPPDATA%\epo。
// 刻意不用 %APPDATA%：漫游目录会导致 DPAPI 密文跨机器解密失败。
func UserDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "epo"), nil
}

// Load 从指定路径读取 profile；文件不存在时返回默认配置。
func Load(path string) (Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return Profile{}, err
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return Profile{}, fmt.Errorf("解析 profile 失败: %w", err)
	}
	if err := p.Validate(); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// Validate 校验必填字段，避免带着坏配置去发请求。
func (p Profile) Validate() error {
	if p.Portal.Host == "" {
		return fmt.Errorf("portal.host 不能为空")
	}
	if p.Portal.Port <= 0 || p.Portal.Port > 65535 {
		return fmt.Errorf("portal.port 非法: %d", p.Portal.Port)
	}
	if p.Network.SSID == "" {
		return fmt.Errorf("network.ssid 不能为空")
	}
	if p.Network.PrimaryProbe == "" {
		return fmt.Errorf("network.primary_probe 不能为空")
	}
	if p.Login.Carrier == "" {
		return fmt.Errorf("login.carrier 不能为空")
	}
	return nil
}
