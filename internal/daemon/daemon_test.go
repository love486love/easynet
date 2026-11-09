package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/love486love/easynet/internal/config"
	"github.com/love486love/easynet/internal/portal"
	"github.com/love486love/easynet/internal/probe"
)

type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return ctx.Err()
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func policy() config.PolicyConfig {
	return config.PolicyConfig{
		OnlineIntervalSec:  5,
		OfflineIntervalSec: 3,
		BackoffMaxSec:      60,
		MaxLoginsPer10Min:  3,
		ShortSessionSec:    60,
	}
}

func TestAlreadyOnlineDoesNotLogin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
		if len(clock.sleeps) >= 3 {
			cancel()
		}
		return portal.StateOnline, probe.Evidence{}, nil
	}

	_ = Run(ctx, Deps{
		Policy: policy(),
		Probe:  probeFn,
		Login: func(context.Context, probe.Evidence) (portal.Result, error) {
			logins++
			return portal.Result{OK: true}, nil
		},
		Sleep: clock.Sleep,
		Now:   clock.Now,
		Log:   quietLogger(),
	})

	if logins != 0 {
		t.Fatalf("已在线时不应登录，实际 %d 次", logins)
	}
	if len(clock.sleeps) == 0 || clock.sleeps[0] != 5*time.Second {
		t.Fatalf("在线探测间隔应为 5s，实际 %v", clock.sleeps)
	}
}

func TestOfflineLoginAndVerify(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0
	probeCalls := 0

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
		probeCalls++
		if probeCalls == 1 {
			return portal.StateOffline, probe.Evidence{Location: "http://172.16.121.9/a79.htm"}, nil
		}
		cancel()
		return portal.StateOnline, probe.Evidence{}, nil
	}

	_ = Run(ctx, Deps{
		Policy: policy(),
		Probe:  probeFn,
		Login: func(context.Context, probe.Evidence) (portal.Result, error) {
			logins++
			return portal.Result{OK: true, Message: "Portal协议认证成功！"}, nil
		},
		Sleep: clock.Sleep,
		Now:   clock.Now,
		Log:   quietLogger(),
	})

	if logins != 1 {
		t.Fatalf("应登录 1 次，实际 %d", logins)
	}
}

func TestFailuresEscalateBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
		if len(clock.sleeps) >= 4 {
			cancel()
		}
		return portal.StateOffline, probe.Evidence{}, nil
	}

	_ = Run(ctx, Deps{
		Policy: policy(),
		Probe:  probeFn,
		Login: func(context.Context, probe.Evidence) (portal.Result, error) {
			logins++
			return portal.Result{OK: false, Message: "账号不存在"}, nil
		},
		Sleep: clock.Sleep,
		Now:   clock.Now,
		Log:   quietLogger(),
	})

	if logins < 3 {
		t.Fatalf("应至少尝试 3 次，实际 %d", logins)
	}
	foundLongWait := false
	for _, d := range clock.sleeps {
		if d >= 45*time.Second {
			foundLongWait = true
		}
	}
	if !foundLongWait {
		t.Fatalf("连续失败后应进入长退避，实际等待序列 %v", clock.sleeps)
	}
}

func TestRateLimitBlocksFourthLogin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
		if logins >= 4 || len(clock.sleeps) >= 6 {
			cancel()
		}
		return portal.StateOffline, probe.Evidence{}, nil
	}

	_ = Run(ctx, Deps{
		Policy: policy(),
		Probe:  probeFn,
		Login: func(context.Context, probe.Evidence) (portal.Result, error) {
			logins++
			return portal.Result{OK: false}, nil
		},
		Sleep: clock.Sleep,
		Now:   clock.Now,
		Log:   quietLogger(),
	})

	if logins > 3 {
		t.Fatalf("10 分钟内不应超过 3 次登录，实际 %d", logins)
	}
}

func TestShortSessionTriggersLongBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	calls := 0
	loginCalls := 0

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
		calls++
		if len(clock.sleeps) >= 6 && calls > 3 {
			cancel()
		}
		switch calls {
		case 1: // 首轮离线 → 登录
			return portal.StateOffline, probe.Evidence{}, nil
		case 2: // 登录后复核成功
			return portal.StateOnline, probe.Evidence{}, nil
		default: // 存活不足 60s 又掉线
			return portal.StateOffline, probe.Evidence{}, nil
		}
	}

	_ = Run(ctx, Deps{
		Policy: policy(),
		Probe:  probeFn,
		Login: func(context.Context, probe.Evidence) (portal.Result, error) {
			loginCalls++
			if loginCalls == 1 {
				return portal.Result{OK: true}, nil
			}
			return portal.Result{OK: false, Message: "会话异常"}, nil
		},
		Sleep: clock.Sleep,
		Now:   clock.Now,
		Log:   quietLogger(),
	})

	found := false
	for _, d := range clock.sleeps {
		if d >= 45*time.Second {
			found = true
		}
	}
	if !found {
		t.Fatalf("短存活应触发长退避，实际等待序列 %v", clock.sleeps)
	}
}

func TestUnknownBelowThresholdDoesNotLogin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
		// 保持 unknown 次数刚好不足阈值，验证不会提前开火。
		if len(clock.sleeps) >= unknownLoginAfter-2 {
			cancel()
		}
		return portal.StateUnknown, probe.Evidence{}, errors.New("lookup www.baidu.com: no such host")
	}

	_ = Run(ctx, Deps{
		Policy: policy(),
		Probe:  probeFn,
		Login: func(context.Context, probe.Evidence) (portal.Result, error) {
			logins++
			return portal.Result{OK: true}, nil
		},
		Sleep: clock.Sleep,
		Now:   clock.Now,
		Log:   quietLogger(),
	})

	if logins != 0 {
		t.Fatalf("探针不可用未达阈值时不应登录，实际 %d 次", logins)
	}
}

func TestUnknownStreakTriggersTrialLogin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0
	probes := 0

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
		probes++
		if logins >= 1 {
			cancel()
		}
		return portal.StateUnknown, probe.Evidence{}, errors.New("lookup www.baidu.com: no such host")
	}

	_ = Run(ctx, Deps{
		Policy: policy(),
		Probe:  probeFn,
		Login: func(context.Context, probe.Evidence) (portal.Result, error) {
			logins++
			return portal.Result{OK: true, Message: "Portal协议认证成功！"}, nil
		},
		Sleep: clock.Sleep,
		Now:   clock.Now,
		Log:   quietLogger(),
	})

	if logins != 1 {
		t.Fatalf("探针持续不可用时应尝试登录 1 次，实际 %d 次（探测 %d 次）", logins, probes)
	}
	if len(clock.sleeps) < unknownLoginAfter-1 {
		t.Fatalf("应等待约 %d 轮后再登录，实际等待序列 %v", unknownLoginAfter-1, clock.sleeps)
	}
	for _, d := range clock.sleeps[:unknownLoginAfter-1] {
		if d != 5*time.Second {
			t.Fatalf("等待期间应按在线间隔轮询（5s），实际 %v", clock.sleeps[:unknownLoginAfter-1])
		}
	}
}

func TestAllowLoginWindow(t *testing.T) {
	start := time.Unix(0, 0)
	attempts := []time.Time{start, start.Add(time.Minute), start.Add(2 * time.Minute)}
	ok, _ := allowLogin(attempts, start.Add(3*time.Minute), 3)
	if ok {
		t.Fatal("窗口内已有 3 次时应拒绝")
	}
	ok, kept := allowLogin(attempts, start.Add(12*time.Minute), 3)
	if !ok || len(kept) != 0 {
		t.Fatalf("超出窗口后应放行并清空历史，ok=%v kept=%d", ok, len(kept))
	}
}

// 登录失败的网络错误会携带完整登录网址（query 含明文密码），
// loginReason 是它进入日志的唯一通道，必须保证输出已脱敏。
func TestLoginReasonMasksPassword(t *testing.T) {
	err := errors.New(`Get "http://172.16.121.9:801/eportal/portal/login?user_account=%2C0%2C2023001%40cmcc&user_password=TopSecret123&v=9": dial tcp: i/o timeout`)
	got := loginReason(portal.Result{}, err)
	if strings.Contains(got, "TopSecret123") {
		t.Fatalf("登录原因泄漏明文密码: %s", got)
	}
	if !strings.Contains(got, "user_password=***") {
		t.Fatalf("敏感参数应被打码，得到: %s", got)
	}
	if !strings.Contains(got, "i/o timeout") {
		t.Fatalf("脱敏后仍应保留排障信息，得到: %s", got)
	}

	got = loginReason(portal.Result{Message: "失败?user_password=abc123&x=1"}, nil)
	if strings.Contains(got, "abc123") {
		t.Fatalf("门户消息未脱敏: %s", got)
	}
}
