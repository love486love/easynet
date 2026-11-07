// Package daemon 实现在线守护循环。
// 状态机与节奏完全由 policy 驱动，依赖以函数注入，便于用假时钟做单测。
package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/love486love/epo-2026/internal/config"
	"github.com/love486love/epo-2026/internal/portal"
	"github.com/love486love/epo-2026/internal/probe"
)

// Deps 是守护循环的全部外部依赖。
type Deps struct {
	Policy config.PolicyConfig
	Probe  func(context.Context) (portal.State, probe.Evidence, error)
	Login  func(context.Context, probe.Evidence) (portal.Result, error)
	Sleep  func(context.Context, time.Duration) error
	Now    func() time.Time
	Log    *slog.Logger
}

const (
	rateWindow     = 10 * time.Minute
	verifyRetries  = 2
	verifyInterval = 500 * time.Millisecond
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
	wasOnline := false

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		state, ev, perr := d.Probe(ctx)
		if perr != nil {
			d.Log.Warn("探针失败", "err", perr)
		}

		switch state {
		case portal.StateOnline:
			if !wasOnline {
				d.Log.Info("网络在线")
			}
			wasOnline = true
			consecutiveFails = 0
			if err := d.Sleep(ctx, onlineEvery); err != nil {
				return nil
			}
			continue

		case portal.StateUnknown:
			// 探针不可用：不当作掉线，避免盲目重登。
			if err := d.Sleep(ctx, onlineEvery); err != nil {
				return nil
			}
			continue
		}

		// 以下按未认证处理。
		if wasOnline {
			d.Log.Warn("检测到掉线")
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

func loginReason(res portal.Result, err error) string {
	if err != nil {
		return err.Error()
	}
	if res.Message != "" {
		return res.Message
	}
	return "未知原因"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
