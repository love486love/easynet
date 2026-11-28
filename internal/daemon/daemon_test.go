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
			// 用无法归类的文案，验证未归类失败仍按普通退避重试。
			return portal.Result{OK: false, Message: "网关拒绝登录"}, nil
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

// 已连上目标校园网（SSID 匹配）时，探针连续 3 次不可用即可判定离线，
// 不必等满 6 次兜底。
func TestUnknownSSIDMatchedTrialsInThree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
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
			return portal.Result{OK: true}, nil
		},
		Sleep:       clock.Sleep,
		Now:         clock.Now,
		Log:         quietLogger(),
		TargetSSIDs: func() []string { return []string{"JXAU-Stu"} },
		CurrentSSID: func() string { return "JXAU-Stu" },
	})

	if logins != 1 {
		t.Fatalf("SSID 匹配时应登录 1 次，实际 %d", logins)
	}
	if len(clock.sleeps) < ssidLoginAfter-1 {
		t.Fatalf("SSID 匹配应 %d 次探不通即登录，实际只等到 %d 轮", ssidLoginAfter, len(clock.sleeps))
	}
	for _, d := range clock.sleeps[:ssidLoginAfter-1] {
		if d != 5*time.Second {
			t.Fatalf("判定期间应按在线间隔轮询（5s），实际 %v", clock.sleeps)
		}
	}
}

// 本部校区 SSID（JXAU，在目标列表第二项）同样触发 3 次判离线。
func TestUnknownSSIDSecondCampusTrialsInThree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
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
			return portal.Result{OK: true}, nil
		},
		Sleep:       clock.Sleep,
		Now:         clock.Now,
		Log:         quietLogger(),
		TargetSSIDs: func() []string { return []string{"JXAU-Stu", "JXAU"} },
		CurrentSSID: func() string { return "JXAU" },
	})

	if logins != 1 {
		t.Fatalf("本部 SSID 在目标列表中应登录 1 次，实际 %d", logins)
	}
	// 走 SSID 快速路径（3 次判离线），而不是 6 次兜底：
	// 判定期间的轮询次数应明显少于兜底阈值所需。
	if len(clock.sleeps) >= unknownLoginAfter-1 {
		t.Fatalf("应走 SSID 快速判离线路径，实际等了 %d 轮（兜底阈值 %d）",
			len(clock.sleeps), unknownLoginAfter)
	}
}

// 连接的不是目标校园网时不代持登录，等用户切回目标网络重新计数。
func TestUnknownSSIDMismatchWaits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
		// 远超两档阈值仍不应开火
		if len(clock.sleeps) >= 12 {
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
		Sleep:       clock.Sleep,
		Now:         clock.Now,
		Log:         quietLogger(),
		TargetSSIDs: func() []string { return []string{"JXAU-Stu"} },
		CurrentSSID: func() string { return "OtherNet" },
	})

	if logins != 0 {
		t.Fatalf("SSID 不匹配时不应登录，实际 %d 次", logins)
	}
}

// 探针已判定离线，但当前明确连着非设定校区（热点、对方校区）时，
// 仍然不发起登录：连什么网络是用户的自由，守护只在自己设定的校园网里认证。
func TestOfflineSkipsLoginOffCampus(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
		if len(clock.sleeps) >= 6 {
			cancel()
		}
		return portal.StateOffline, probe.Evidence{Location: "http://172.16.121.9/a79.htm"}, nil
	}

	_ = Run(ctx, Deps{
		Policy: policy(),
		Probe:  probeFn,
		Login: func(context.Context, probe.Evidence) (portal.Result, error) {
			logins++
			return portal.Result{OK: true}, nil
		},
		Sleep:       clock.Sleep,
		Now:         clock.Now,
		Log:         quietLogger(),
		TargetSSIDs: func() []string { return []string{"JXAU-Stu"} },
		CurrentSSID: func() string { return "MyPhoneHotspot" },
	})

	if logins != 0 {
		t.Fatalf("非设定校区不应发起登录，实际 %d 次", logins)
	}
	// 跳过期间只按在线节奏等待：不进入失败退避，也不消耗登录配额。
	for _, d := range clock.sleeps {
		if d != 5*time.Second {
			t.Fatalf("跳过时应按 5s 节奏等待，实际 %v", clock.sleeps)
		}
	}
}

// 反向确认：明确连着设定校区时，离线判定仍要正常登录，前置判断不能误伤。
func TestOfflineLoginsOnTargetCampus(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0

	_ = Run(ctx, Deps{
		Policy: policy(),
		Probe: func(context.Context) (portal.State, probe.Evidence, error) {
			return portal.StateOffline, probe.Evidence{}, nil
		},
		Login: func(context.Context, probe.Evidence) (portal.Result, error) {
			logins++
			cancel()
			return portal.Result{OK: true}, nil
		},
		Sleep:       clock.Sleep,
		Now:         clock.Now,
		Log:         quietLogger(),
		TargetSSIDs: func() []string { return []string{"JXAU-Stu"} },
		CurrentSSID: func() string { return "JXAU-Stu" },
	})

	if logins != 1 {
		t.Fatalf("设定校区内应登录 1 次，实际 %d 次", logins)
	}
}

// offCampus 的边界：只有"明确连着别的网络"才拦截，读不到 SSID 时不拦截。
func TestOffCampus(t *testing.T) {
	cases := []struct {
		name    string
		cur     string
		targets []string
		wantOff bool
	}{
		{"热点", "MyPhoneHotspot", []string{"JXAU-Stu"}, true},
		{"设定校区", "JXAU-Stu", []string{"JXAU-Stu"}, false},
		{"大小写与空白", " jxau-stu ", []string{"JXAU-Stu"}, false},
		{"未连 Wi-Fi", "", []string{"JXAU-Stu"}, false},
		{"无目标列表", "X", nil, false},
	}
	for _, c := range cases {
		d := Deps{
			TargetSSIDs: func() []string { return c.targets },
			CurrentSSID: func() string { return c.cur },
		}
		if _, got := d.offCampus(); got != c.wantOff {
			t.Errorf("%s: offCampus=%v，期望 %v", c.name, got, c.wantOff)
		}
	}
	// 没有 SSID 读取能力时无法判定，不应拦截登录。
	if _, got := (Deps{}).offCampus(); got {
		t.Error("未提供 CurrentSSID 时不应判定为跨校区")
	}
}

// 门户 TCP 走不通（开机链路未就绪）时即使阈值已满也继续等待，
// 不发起注定失败的空参数盲登。
func TestUnknownPortalUnreachableWaits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
		if len(clock.sleeps) >= 12 {
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
		Sleep:       clock.Sleep,
		Now:         clock.Now,
		Log:         quietLogger(),
		TargetSSIDs: func() []string { return []string{"JXAU-Stu"} },
		CurrentSSID: func() string { return "JXAU-Stu" },
		PortalReachable: func(context.Context) bool {
			return false // 链路未就绪，门户连不上
		},
	})

	if logins != 0 {
		t.Fatalf("门户不可达时不应登录，实际 %d 次", logins)
	}
}

// 链路未就绪类错误（请求没到网关）不消耗登录频率配额、不进认证退避：
// 连续多次后仍可立即重试，不会像网关拒绝那样被 10 分钟 3 次限住。
func TestLinkNotReadyDoesNotConsumeQuota(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
		if logins >= 5 || len(clock.sleeps) >= 20 {
			cancel()
		}
		return portal.StateOffline, probe.Evidence{}, nil
	}

	_ = Run(ctx, Deps{
		Policy: policy(),
		Probe:  probeFn,
		Login: func(context.Context, probe.Evidence) (portal.Result, error) {
			logins++
			return portal.Result{}, errors.New(`dial tcp 172.16.121.9:801: connectex: A socket operation was attempted to an unreachable network`)
		},
		Sleep: clock.Sleep,
		Now:   clock.Now,
		Log:   quietLogger(),
	})

	if logins < 5 {
		t.Fatalf("链路错误不应触发频率限制，应可连续尝试，实际 %d 次（配额 3 次/10分钟）", logins)
	}
	for _, d := range clock.sleeps {
		if d >= 45*time.Second {
			t.Fatalf("链路错误不应进入认证退避，实际等待序列 %v", clock.sleeps)
		}
	}
}

func TestIsLinkNotReady(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"dial tcp 172.16.121.9:801: connectex: A socket operation was attempted to an unreachable network", true},
		{"lookup www.baidu.com: no such host", true},
		{"dial tcp 172.16.121.9:801: connectex: No connection could be made because the target machine actively refused it", true},
		{"net/http: request canceled (Client.Timeout exceeded)", true},
		{`Get "http://x/": context deadline exceeded`, true},
		{"HTTP 500: 服务器内部错误", false},
		{"账号或密码错误", false},
		{"", false},
	}
	for _, c := range cases {
		var err error
		if c.msg != "" {
			err = errors.New(c.msg)
		}
		if got := isLinkNotReady(err); got != c.want {
			t.Errorf("isLinkNotReady(%q) = %v, want %v", c.msg, got, c.want)
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

// 网关提示不在上网时段时进入固定静默：期间只探测不登录。
func TestTimePolicyEntersSilence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0
	type status struct {
		reason  string
		retryAt time.Time
	}
	var got []status

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
		if len(clock.sleeps) >= 30 {
			cancel()
		}
		return portal.StateOffline, probe.Evidence{}, nil
	}

	_ = Run(ctx, Deps{
		Policy: policy(),
		Probe:  probeFn,
		Login: func(context.Context, probe.Evidence) (portal.Result, error) {
			logins++
			return portal.Result{Code: 2, Message: "当前不在允许上网的时间段"}, nil
		},
		Sleep: clock.Sleep,
		Now:   clock.Now,
		Log:   quietLogger(),
		SetStatus: func(reason, _ string, retryAt time.Time) {
			got = append(got, status{reason, retryAt})
		},
	})

	if logins != 1 {
		t.Fatalf("静默期间不应再次登录，实际 %d 次", logins)
	}
	if len(got) == 0 || got[0].reason != "time_policy" {
		t.Fatalf("应写入 time_policy 暂停原因，实际 %+v", got)
	}
	wantRetry := time.Unix(0, 0).Add(timePolicySilence)
	if !got[0].retryAt.Equal(wantRetry) {
		t.Fatalf("恢复时间应为 %v，实际 %v", wantRetry, got[0].retryAt)
	}
	// 静默期间按在线节奏轮询，不进入失败退避。
	for _, d := range clock.sleeps {
		if d != 5*time.Second {
			t.Fatalf("静默期间应按 5s 轮询，实际 %v", clock.sleeps)
		}
	}
}

// 密码错误后停止自动重试，避免反复尝试加重锁定。
func TestBadCredentialStopsRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0
	statusReason := ""

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
		if len(clock.sleeps) >= 20 {
			cancel()
		}
		return portal.StateOffline, probe.Evidence{}, nil
	}

	_ = Run(ctx, Deps{
		Policy: policy(),
		Probe:  probeFn,
		Login: func(context.Context, probe.Evidence) (portal.Result, error) {
			logins++
			return portal.Result{Code: 2, Message: "用户不存在或者密码错误!"}, nil
		},
		Sleep: clock.Sleep,
		Now:   clock.Now,
		Log:   quietLogger(),
		SetStatus: func(reason, _ string, _ time.Time) {
			statusReason = reason
		},
	})

	if logins != 1 {
		t.Fatalf("密码错误后不应继续重试，实际登录 %d 次", logins)
	}
	if statusReason != "credential" {
		t.Fatalf("应写入 credential 暂停原因，实际 %q", statusReason)
	}
}

// 重新保存账号（文件版本变化）后恢复自动认证，并立即重试一次。
func TestCredentialChangeResumes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0
	version := "v1"

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
		if logins >= 2 {
			cancel()
		}
		// 首次被拒后模拟用户重新保存账号：文件版本变化。
		if logins == 1 {
			version = "v2"
		}
		return portal.StateOffline, probe.Evidence{}, nil
	}

	_ = Run(ctx, Deps{
		Policy: policy(),
		Probe:  probeFn,
		Login: func(context.Context, probe.Evidence) (portal.Result, error) {
			logins++
			if logins == 1 {
				return portal.Result{Code: 2, Message: "密码错误"}, nil
			}
			return portal.Result{OK: true}, nil
		},
		Sleep:             clock.Sleep,
		Now:               clock.Now,
		Log:               quietLogger(),
		CredentialVersion: func() string { return version },
	})

	if logins != 2 {
		t.Fatalf("账号更新后应恢复并重试 1 次，实际登录 %d 次", logins)
	}
}

// 并发退避：30s 起翻倍，封顶 10 分钟。
func TestConcurrencyBackoffValues(t *testing.T) {
	want := []time.Duration{
		30 * time.Second, time.Minute, 2 * time.Minute,
		4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute,
	}
	for i, w := range want {
		if got := concurrencyBackoff(i + 1); got != w {
			t.Errorf("第 %d 次退避应为 %v，实际 %v", i+1, w, got)
		}
	}
}

// 并发被拒时守护确实走长退避，而不是快速重试。
func TestConcurrencyRejectSlowsDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{now: time.Unix(0, 0)}
	logins := 0

	probeFn := func(context.Context) (portal.State, probe.Evidence, error) {
		if logins >= 10 {
			cancel()
		}
		return portal.StateOffline, probe.Evidence{}, nil
	}

	_ = Run(ctx, Deps{
		Policy: policy(),
		Probe:  probeFn,
		Login: func(context.Context, probe.Evidence) (portal.Result, error) {
			logins++
			return portal.Result{Code: 2, Message: "该账号同时在线数已达上限"}, nil
		},
		Sleep: clock.Sleep,
		Now:   clock.Now,
		Log:   quietLogger(),
	})

	hasBase, hasMax := false, false
	for _, d := range clock.sleeps {
		switch d {
		case concurrencyBackoffBase:
			hasBase = true
		case concurrencyBackoffMax:
			hasMax = true
		}
	}
	if !hasBase || !hasMax {
		t.Fatalf("并发被拒应逐步退避到 %v，实际等待序列 %v", concurrencyBackoffMax, clock.sleeps)
	}
}
