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

// Probe 是一个探针：url 为地址，expect 为正常状态下正文中必然出现的片段。
// expect 必须能在前 8 KB 内命中，且建议使用 ASCII 片段（页面多为 GBK 编码）。
type Probe struct {
	URL    string `json:"url"`
	Expect string `json:"expect"`
}

type NetworkConfig struct {
	SSID       string   `json:"ssid"`
	Probes     []Probe  `json:"probes"`
	ProbeHosts []string `json:"probe_hosts"`
}

type LoginConfig struct {
	Carrier         string            `json:"carrier"`
	CarrierSuffixes map[string]string `json:"carrier_suffixes"`
	ExtraParams     map[string]string `json:"extra_params"`
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
			SSID: "JXAU-Stu",
			Probes: []Probe{
				{URL: "http://www.jxau.edu.cn/?epo=1", Expect: "jxau.edu.cn"},
				{URL: "http://www.baidu.com/?epo=1", Expect: "baidu.com"},
			},
			ProbeHosts: []string{"www.jxau.edu.cn", "jxau.edu.cn"},
		},
		Login: LoginConfig{
			Carrier: "cmcc",
			// 移动实测 @cmcc；电信/联通取自同类 ePortal 项目实测值，待本地验证。
			CarrierSuffixes: map[string]string{
				"cmcc":   "cmcc",
				"ctcc":   "telecom",
				"cucc":   "unicom",
				"campus": "",
			},
			ExtraParams: map[string]string{
				"jsVersion":     "4.2.1",
				"terminal_type": "1",
				"login_method":  "1",
				"lang":          "zh-cn",
			},
		},
		Policy: PolicyConfig{
			// 探测间隔直接决定掉线恢复速度，参考同类项目实测值取 5 秒。
			OnlineIntervalSec:  5,
			OfflineIntervalSec: 3,
			BackoffMaxSec:      60,
			MaxLoginsPer10Min:  3,
			ShortSessionSec:    60,
			RetryOnAuthFail:    false,
		},
	}
}

// UserDir 返回 %LOCALAPPDATA%\epo。
// 刻意不用 %APPDATA%（漫游）：漫游目录会随账号同步到其它机器，
// 而 DPAPI 密文绑定本机，同步过去反而解密失败。
func UserDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", fmt.Errorf("环境变量 LOCALAPPDATA 未设置")
	}
	return filepath.Join(base, "epo"), nil
}

// LogDir 返回日志目录：优先程序目录下的 log（不占系统盘、随程序一起删除），
// 程序目录不可写（例如装在 Program Files）时回退到 %LOCALAPPDATA%\epo\logs。
func LogDir() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "log")
		if mkErr := os.MkdirAll(dir, 0o755); mkErr == nil {
			probe := filepath.Join(dir, ".write-test")
			if werr := os.WriteFile(probe, []byte("x"), 0o644); werr == nil {
				_ = os.Remove(probe)
				return dir
			}
		}
	}
	if d, err := UserDir(); err == nil {
		return filepath.Join(d, "logs")
	}
	return filepath.Join(os.TempDir(), "epo", "logs")
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
	if len(p.Network.Probes) == 0 {
		return fmt.Errorf("network.probes 不能为空")
	}
	if p.Login.Carrier == "" {
		return fmt.Errorf("login.carrier 不能为空")
	}
	return nil
}
