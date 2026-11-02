// Package drcom 实现 Dr.COM ePortal 的登录协议。
// 协议细节全部集中在本包，上层不感知任何参数名。
package drcom

import (
	"fmt"
	"math/rand"
	"net/url"
	"strings"

	"github.com/love486love/epo-2026/internal/portal"
)

// 运营商后缀。移动实测为 @cmcc；电信/联通沿用 Dr.COM 惯例，尚未实测。
var carrierSuffix = map[string]string{
	"cmcc":   "cmcc",
	"ctcc":   "dx",
	"cucc":   "lt",
	"campus": "",
}

// LoginParams 是构造登录请求所需的全部输入。
type LoginParams struct {
	Credential portal.Credential
	UserIP     string // 优先取 302 的 Location.userip
	UserMAC    string // 302 的 Location.usermac，必须原样回传
	AcIP       string // 302 的 Location.wlanacip
	Extra      map[string]string
}

func extraOr(m map[string]string, key, def string) string {
	if m != nil {
		if v, ok := m[key]; ok && v != "" {
			return v
		}
	}
	return def
}

// Account 生成 user_account，格式为 ",0,<学号>[@<运营商后缀>]"。
func (p LoginParams) Account() (string, error) {
	carrier := strings.ToLower(strings.TrimSpace(p.Credential.Carrier))
	suffix, ok := carrierSuffix[carrier]
	if !ok {
		return "", fmt.Errorf("未知运营商 %q", p.Credential.Carrier)
	}
	account := ",0," + p.Credential.Username
	if suffix != "" {
		account += "@" + suffix
	}
	return account, nil
}

// URL 按 docs/TECH_DESIGN.md §6.3 构造登录地址。
func (p LoginParams) URL(ep portal.Endpoint) (string, error) {
	if ep.Host == "" {
		return "", fmt.Errorf("门户主机为空")
	}
	account, err := p.Account()
	if err != nil {
		return "", err
	}

	path := ep.LoginPath
	if path == "" {
		path = "/eportal/portal/login"
	}

	q := url.Values{}
	q.Set("callback", "dr1003")
	q.Set("login_method", extraOr(p.Extra, "login_method", "1"))
	q.Set("user_account", account)
	q.Set("user_password", p.Credential.Password)
	q.Set("wlan_user_ip", p.UserIP)
	q.Set("wlan_user_ipv6", "")
	q.Set("wlan_user_mac", p.UserMAC)
	q.Set("wlan_ac_ip", p.AcIP)
	q.Set("wlan_ac_name", "")
	q.Set("jsVersion", extraOr(p.Extra, "jsVersion", "4.2.1"))
	q.Set("terminal_type", extraOr(p.Extra, "terminal_type", "1"))
	q.Set("lang", extraOr(p.Extra, "lang", "zh-cn"))
	q.Set("v", fmt.Sprintf("%d", 1000+rand.Intn(9000)))

	return fmt.Sprintf("http://%s:%d%s?%s", ep.Host, ep.Port, path, q.Encode()), nil
}
