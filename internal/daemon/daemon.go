// Package daemon 实现在线守护循环。
// 状态机与节奏完全由 policy 驱动，依赖以函数注入，便于用假时钟做单测。
package daemon

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/love486love/easynet/internal/config"
	"github.com/love486love/easynet/internal/logx"
	"github.com/love486love/easynet/internal/portal"
	"github.com/love486love/easynet/internal/probe"
)

// Deps 是守护循环的全部外部依赖。
type Deps struct {
	Policy config.PolicyConfig
	Probe  func(context.Context) (portal.State, probe.Evidence, error)
	Login  func(context.Context, probe.Evidence) (portal.Result, error)
	Sleep  func(context.Context, time.Duration) error
	Now    func() time.Time
	Log    *slog.Logger
	// Stop 返回 true 时安全退出循环，用于响应 GUI 的停止请求。
	Stop func() bool
	// OnVerified 在"登录成功且探针复核在线"后调用，用于记录 TOFU 网关 MAC。
	OnVerified func()
	// TargetSSIDs 是 profile 中的目标校园网 SSID 列表（东区 JXAU-Stu +
	// 本部 JXAU）；CurrentSSID 返回当前连接的 SSID（取不到返回空）。
	// SSID 匹配是最快的离线信号：已连上任一目标校园网却探不通，
	// 大概率是没认证，无需等满兜底次数。
	TargetSSIDs []string
	CurrentSSID func() string
	// PortalReachable 在试探登录前探测门户可达（nil 视为可达）。
	// 开机阶段 Wi-Fi 已关联但 DHCP/DNS 未就绪时门户走不通，
	// 先等链路就绪再登录，避免发注定失败的空参数请求。
	PortalReachable func(context.Context) bool
}

const (
	rateWindow     = 10 * time.Minute
	verifyRetries  = 2
	verifyInterval = 500 * time.Millisecond
	// unknownLoginAfter 是连续探针不可用多少次后允许一次试探登录（兜底）。
	// 开机时 DHCP/DNS 往往还没就绪，探针会一直报错而登录本身走裸 IP，
	// 不依赖 DNS，因此不能一直停在"无法判定"上。
	unknownLoginAfter = 6
	// ssidLoginAfter 是已确认连上目标校园网时的判定次数（SSID 是最快的
	// 离线信号，比等满兜底次数快一倍以上）。
	ssidLoginAfter = 3
)

// Run 阻塞运行守护循环，直到 ctx 取消。
// 节奏：在线按 online_interval_sec 探测；掉线立即登录；失败按 offline_interval_sec
// 快速重试，连续失败 3 次后升到 45s，5 次以上封顶 backoff_max_sec。
func Run(ctx context.Context, d Deps) error {
	d = withDefaults(d)
	policy := d.Policy

	onlineEvery := seconds(policy.OnlineIntervalSec, 5)
	failRetry := seconds(policy.OfflineIntervalSec, 3)
	backoffMax := seconds(policy.BackoffMaxSec, 60)
	shortSession := seconds(policy.ShortSessionSec, 60)
	maxLogins := policy.MaxLoginsPer10Min
	if maxLogins <= 0 {
		maxLogins = 3
	}

	var attempts []time.Time
	var lastLoginAt time.Time
	consecutiveFails := 0
	unknownStreak := 0
	probeFails := 0
	wasOnline := false

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if d.Stop != nil && d.Stop() {
			d.Log.Info("收到停止请求，退出守护")
			return nil
		}

		state, ev, perr := d.Probe(ctx)
		if perr != nil {
			probeFails++
			d.Log.Warn("探针失败", "连续次数", probeFails, "err", perr)
		} else if probeFails > 0 {
			// 从失败恢复成功也是一次状态变化，记下来才能还原完整时间线。
			d.Log.Info("探针恢复", "此前连续失败", probeFails)
			probeFails = 0
		}

		switch state {
		case portal.StateOnline:
			if !wasOnline {
				d.Log.Info("网络在线")
			}
			wasOnline = true
			consecutiveFails = 0
			unknownStreak = 0
			if err := d.Sleep(ctx, onlineEvery); err != nil {
				return nil
			}
			continue

		case portal.StateUnknown:
			// 探针不可用先按原样等待；持续不可用则视为"很可能没认证"，
			// 尝试登录一次。登录请求走门户裸 IP，不依赖 DNS 解析。
			unknownStreak++
			if unknownStreak == 1 {
				d.Log.Info("探针无法判定网络状态", "http", ev.StatusCode, "err", perr)
			}
			// 判定阈值分三档：已连目标校园网 3 次即可判离线（SSID 是
			// 最快信号）；连的是别的网络不代持登录，等切回来重新计数；
			// SSID 取不到（未连 Wi-Fi/netsh 失败）保留 6 次兜底。
			need := unknownLoginAfter
			if d.CurrentSSID != nil && len(d.TargetSSIDs) > 0 {
				switch cur := strings.TrimSpace(d.CurrentSSID()); {
				case cur == "":
					// 未连 Wi-Fi 或查询失败：走兜底阈值
				case matchSSID(cur, d.TargetSSIDs):
					need = ssidLoginAfter
				default:
					if unknownStreak == 1 {
						d.Log.Info("当前 Wi-Fi 非目标校园网，暂不发起登录",
							"当前SSID", cur, "目标SSID", strings.Join(d.TargetSSIDs, ","))
					}
					unknownStreak = 0
					if err := d.Sleep(ctx, onlineEvery); err != nil {
						return nil
					}
					continue
				}
			}
			if unknownStreak < need {
				if err := d.Sleep(ctx, onlineEvery); err != nil {
					return nil
				}
				continue
			}
			// 门户走不通（开机链路未就绪）就继续等，不发注定失败的盲登。
			if d.PortalReachable != nil && !d.PortalReachable(ctx) {
				d.Log.Info("门户尚未可达，继续等待网络就绪", "连续次数", need)
				unknownStreak = 0
				if err := d.Sleep(ctx, onlineEvery); err != nil {
					return nil
				}
				continue
			}
			unknownStreak = 0
			d.Log.Warn("探针持续不可用，尝试直接登录", "连续次数", need)
		}

		// 以下按未认证处理。
		if wasOnline {
			d.Log.Warn("检测到掉线", "判定", state.String(), "http", ev.StatusCode)
			wasOnline = false
			// 短存活：刚登录就掉，说明会话异常，直接进入长退避。
			if !lastLoginAt.IsZero() && d.Now().Sub(lastLoginAt) < shortSession {
				consecutiveFails = maxInt(consecutiveFails, 3)
				d.Log.Warn("会话存活时间过短，进入长退避")
			}
		}

		var allowed bool
		allowed, attempts = allowLogin(attempts, d.Now(), maxLogins)
		if !allowed {
			d.Log.Warn("触发登录频率上限，暂停", "window", rateWindow.String())
			if err := d.Sleep(ctx, backoffMax); err != nil {
				return nil
			}
			continue
		}

		attempts = append(attempts, d.Now())
		if lastLoginAt.IsZero() {
			lastLoginAt = d.Now()
		}

		res, lerr := d.Login(ctx, ev)
		if lerr != nil && isLinkNotReady(lerr) {
			// 链路未就绪：请求根本没到网关，与凭据/风控无关。
			// 撤销本次配额占用，不进认证退避，按在线节奏等链路恢复。
			attempts = attempts[:len(attempts)-1]
			d.Log.Warn("登录请求未送达门户，等待网络就绪", "err", logx.MaskURL(lerr.Error()))
			if err := d.Sleep(ctx, onlineEvery); err != nil {
				return nil
			}
			continue
		}
		if lerr != nil || !res.OK {
			consecutiveFails++
			wait := backoff(consecutiveFails, failRetry, backoffMax)
			d.Log.Warn("登录失败", "连续失败", consecutiveFails, "等待", wait.String(), "原因", loginReason(res, lerr))
			if err := d.Sleep(ctx, wait); err != nil {
				return nil
			}
			continue
		}

		// 登录成功后短复核，避免把"假成功"当成在线。
		verified := false
		for i := 0; i < verifyRetries; i++ {
			if err := d.Sleep(ctx, verifyInterval); err != nil {
				return nil
			}
			if st, _, err := d.Probe(ctx); err == nil && st == portal.StateOnline {
				verified = true
				break
			}
		}
		if !verified {
			consecutiveFails++
			wait := backoff(consecutiveFails, failRetry, backoffMax)
			d.Log.Warn("登录后复核未通过", "连续失败", consecutiveFails, "等待", wait.String())
			if err := d.Sleep(ctx, wait); err != nil {
				return nil
			}
			continue
		}

		lastLoginAt = d.Now()
		wasOnline = true
		consecutiveFails = 0
		if d.OnVerified != nil {
			d.OnVerified()
		}
		d.Log.Info("登录成功，进入在线态")
	}
}

func withDefaults(d Deps) Deps {
	if d.Sleep == nil {
		d.Sleep = func(ctx context.Context, dur time.Duration) error {
			select {
			case <-time.After(dur):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return d
}

func seconds(v int, def int) time.Duration {
	if v <= 0 {
		v = def
	}
	return time.Duration(v) * time.Second
}

// allowLogin 在 10 分钟窗口内限制登录次数，并顺手清理过期记录。
func allowLogin(attempts []time.Time, now time.Time, max int) (bool, []time.Time) {
	kept := attempts[:0]
	for _, t := range attempts {
		if now.Sub(t) < rateWindow {
			kept = append(kept, t)
		}
	}
	return len(kept) < max, kept
}

// backoff：前两次失败用快速重试，3 次起进入 45s 长退避，5 次以上封顶。
func backoff(fails int, retry, max time.Duration) time.Duration {
	switch {
	case fails < 3:
		return retry
	case fails < 5:
		if max < 45*time.Second {
			return max
		}
		return 45 * time.Second
	default:
		return max
	}
}

// isLinkNotReady 判断登录错误是否为链路层未就绪——请求根本没送达网关
// （开机 Wi-Fi 已关联但 DHCP/DNS/路由尚未完成、门户服务未起等）。
// 这类失败与凭据、风控无关，不消耗登录频率配额，也不进认证退避。
func isLinkNotReady(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, k := range []string{
		"unreachable network", // 路由/链路未就绪（connectex）
		"no such host",        // DNS 未就绪
		"actively refused",    // 门户服务未起（Windows refused 文案）
		"connection refused",  // 门户服务未起（Unix 文案）
		"i/o timeout",         // 传输超时
		"deadline exceeded",   // 请求超时未达
		"Timeout exceeded",    // HTTP 客户端超时未回
	} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

func loginReason(res portal.Result, err error) string {
	// 登录请求失败时，error 可能携带完整登录网址（query 含明文密码），
	// 进日志前统一脱敏，凭据不得出现在任何输出中。
	if err != nil {
		return logx.MaskURL(err.Error())
	}
	if res.Message != "" {
		return logx.MaskURL(res.Message)
	}
	return "未知原因"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// matchSSID 判断当前 SSID 是否属于任一目标校园网（忽略大小写与两端空白）。
func matchSSID(cur string, targets []string) bool {
	for _, t := range targets {
		if strings.EqualFold(cur, strings.TrimSpace(t)) {
			return true
		}
	}
	return false
}
