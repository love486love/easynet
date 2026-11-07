package daemon

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/love486love/epo-2026/internal/config"
	"github.com/love486love/epo-2026/internal/portal"
	"github.com/love486love/epo-2026/internal/probe"
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
