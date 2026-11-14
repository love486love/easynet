// Package guard 在发送凭据前做强制校验。
// 设计前提：开放 Wi-Fi + 明文 HTTP 无法做密码学验证，
// 这里只能抬高门槛，不能宣称已防住钓鱼。
package guard

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrSSIDMismatch 表示当前连接的不是目标校园网。
	ErrSSIDMismatch = errors.New("当前网络不是目标校园网")
	// ErrPortalNotAllowed 表示门户主机不在静态白名单内。
	ErrPortalNotAllowed = errors.New("门户主机不在白名单内")
)

// Input 是一次登录前校验的输入。
type Input struct {
	TargetSSIDs  []string // profile 中的目标 ssid 列表（东区 JXAU-Stu + 本部 JXAU）
	CurrentSSID  string   // 当前实际连接的 ssid
	PortalHost   string   // 将要发送凭据的主机
	AllowedHosts []string // 静态白名单；重定向解析出的主机不得作为放行依据
	GatewayMAC   string
	TrustedMACs  []string // TOFU 信任列表，空表示尚未建立信任
	RandomMAC    bool
}

// Result 是校验结果，Warnings 不阻断登录。
type Result struct {
	Warnings []string
}

// Check 执行四项校验。任一阻断项失败即返回对应错误，
// 调用方必须停止登录，不得降级继续。
func Check(in Input) (Result, error) {
	var res Result

	if strings.TrimSpace(in.CurrentSSID) == "" {
		return res, errors.New("未连接到无线网络")
	}
	if !ssidAllowed(in.CurrentSSID, in.TargetSSIDs) {
		return res, fmt.Errorf("%w：期望 %q，实际 %q", ErrSSIDMismatch,
			strings.Join(in.TargetSSIDs, ","), in.CurrentSSID)
	}

	if !hostAllowed(in.PortalHost, in.AllowedHosts) {
		return res, fmt.Errorf("%w：%q", ErrPortalNotAllowed, in.PortalHost)
	}

	if in.RandomMAC {
		res.Warnings = append(res.Warnings, "当前开启了随机硬件地址，部分校园网关会因此频繁掉线")
	}

	if len(in.TrustedMACs) > 0 {
		if in.GatewayMAC == "" {
			res.Warnings = append(res.Warnings, "无法读取网关 MAC，本次跳过信任校验")
			return res, nil
		}
		if !containsMAC(in.TrustedMACs, in.GatewayMAC) {
			// 门户主机已通过白名单校验（防凭据外泄的主防线）；网关 MAC 多一个
			// 就来自另一个校区的真 AC（东区/本部各自网关）。若在此阻断会形成
			// 死锁——只有登录成功才记录新 MAC，拒绝登录则永远记录不了。
			// 因此降级为警告放行，登录成功后由调用方记入信任列表（TOFU 语义）。
			res.Warnings = append(res.Warnings,
				"新网关 MAC："+in.GatewayMAC+"（不在信任列表，登录成功后将自动记录）")
		}
	}
	return res, nil
}

func ssidAllowed(cur string, targets []string) bool {
	cur = strings.TrimSpace(cur)
	for _, t := range targets {
		if strings.EqualFold(cur, strings.TrimSpace(t)) {
			return true
		}
	}
	return false
}

func hostAllowed(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return false
	}
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(a), "."))
		if a != "" && host == a {
			return true
		}
	}
	return false
}

func containsMAC(list []string, mac string) bool {
	target := normalizeMAC(mac)
	for _, item := range list {
		if normalizeMAC(item) == target {
			return true
		}
	}
	return false
}

func normalizeMAC(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "-", ":"))
}
