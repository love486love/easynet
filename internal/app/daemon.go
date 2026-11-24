package app

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/love486love/easynet/internal/config"
	"github.com/love486love/easynet/internal/creds"
	"github.com/love486love/easynet/internal/daemon"
	"github.com/love486love/easynet/internal/guard"
	"github.com/love486love/easynet/internal/instance"
	"github.com/love486love/easynet/internal/logx"
	"github.com/love486love/easynet/internal/netinfo"
	"github.com/love486love/easynet/internal/portal"
	"github.com/love486love/easynet/internal/portal/drcom"
	"github.com/love486love/easynet/internal/portal/ruijie"
	"github.com/love486love/easynet/internal/probe"
	"github.com/love486love/easynet/internal/session"
	"github.com/love486love/easynet/internal/state"
	"github.com/love486love/easynet/internal/trust"
)

// portalKind 返回协议名，仅用于日志可读性。
func portalKind(ruijiePortal bool) string {
	if ruijiePortal {
		return "ruijie-eportal"
	}
	return "drcom-eportal"
}

// ssidInList 判断当前 SSID 是否属于任一目标校园网（忽略大小写与空白）。
func ssidInList(cur string, targets []string) bool {
	cur = strings.TrimSpace(cur)
	for _, t := range targets {
		if strings.EqualFold(cur, strings.TrimSpace(t)) {
			return true
		}
	}
	return false
}

// RunDaemon 启动常驻守护，直到 ctx 取消、收到停止请求或进程被结束。
// 界面里「守护」开关打开时拉起 `easynet.exe --daemon`，开机自启也指向同一条命令。
func RunDaemon(profilePath string) error {
	prof, err := LoadProfile(profilePath)
	if err != nil {
		return err
	}
	dir, err := config.UserDir()
	if err != nil {
		return err
	}

	// 先建日志，再读凭据：否则早期失败不会留下任何可排查的痕迹。
	logger, closeLog, err := logx.New(config.LogDir())
	if err != nil {
		return err
	}
	defer closeLog()

	// 上一轮残留的停止标记会让本次启动立刻退出，这里先清掉。
	_ = state.ClearStop(dir)

	// 启动只校验"至少保存过一个账号"；具体用哪个校区的账号
	// 由探测到的门户指纹在每次登录时决定（见 loginFn）。
	if store, err := creds.LoadStore(dir); err != nil || len(store.Accounts) == 0 {
		logger.Error("读取凭据失败", "err", err)
		return fmt.Errorf("读取凭据失败（请先在界面保存账号）: %w", err)
	}

	lock, ok, err := instance.Acquire(`Local\easynet-daemon`)
	if err != nil {
		return err
	}
	if !ok {
		logger.Info("已有守护进程在运行，本次退出")
		return nil
	}
	defer lock.Release()

	exitMessage := "守护已退出"
	defer func() {
		_ = state.Write(dir, state.Snapshot{State: "stopped", Message: exitMessage, PID: os.Getpid()})
		_ = state.ClearStop(dir)
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	trusted, err := trust.Load(dir)
	if err != nil {
		exitMessage = "读取网关信任列表失败"
		return fmt.Errorf("读取网关信任列表失败: %w", err)
	}
	lastGatewayMAC := ""
	lastMessage := ""

	// 每轮读磁盘上的最新配置：界面切换校区后无需重启守护即可生效。
	currentProfile := func() config.Profile {
		if p, err := LoadProfile(profilePath); err == nil {
			return p
		}
		return prof
	}

	loginFn := func(ctx context.Context, ev probe.Evidence) (portal.Result, error) {
		cur := currentProfile()
		plan := session.Plan(cur)

		ni, niErr := netinfo.Get()
		if niErr != nil {
			return portal.Result{Code: 6, Message: niErr.Error()}, niErr
		}

		// 只解析本校区门户：另一校区的重定向一律忽略，绝不串校区登录。
		ep, warns := session.EndpointForCampus(cur, ev)
		for _, w := range warns {
			logger.Warn("网关参数告警", "detail", w)
		}

		// 校区账号与协议都由选择决定，不再按探测指纹猜。
		secret, serr := creds.LoadForCampus(dir, plan.Campus)
		if serr != nil {
			// 本地配置问题：走普通失败退避（间隔有上限），日志给出明确指引。
			logger.Warn("校区账号缺失", "校区", plan.Campus, "err", serr)
			return portal.Result{Code: 6, Message: serr.Error()}, serr
		}
		cred := portal.Credential{
			Username: secret.Username,
			Password: secret.Password,
			Carrier:  secret.Carrier,
		}
		if cred.Carrier == "" {
			cred.Carrier = cur.Login.Carrier
		}

		if !plan.UseRuijie {
			// 探针不可用（例如开机时 DNS 尚未就绪）拿不到 302 参数，
			// 用本机网卡信息补齐，避免登录请求带空 userip 被网关拒绝。
			if ep.Extra["userip"] == "" {
				ep.Extra["userip"] = ni.IPv4
			}
			if ep.Extra["usermac"] == "" {
				ep.Extra["usermac"] = ni.MAC
			}
		}

		// 四项校验：SSID / 门户白名单 / 随机 MAC（仅告警）/ 网关 MAC 信任。
		// 白名单只放本校区门户，任何跨校区地址都会被直接拒绝。
		if gres, gerr := guard.Check(guard.Input{
			TargetSSIDs:  plan.SSIDs,
			CurrentSSID:  ni.SSID,
			PortalHost:   ep.Host,
			AllowedHosts: []string{plan.PortalHost},
			GatewayMAC:   ni.GatewayMAC,
			TrustedMACs:  trusted,
			RandomMAC:    ni.RandomMAC,
		}); gerr != nil {
			logger.Warn("安全校验未通过，放弃本次登录", "err", gerr)
			return portal.Result{Code: 6, Message: gerr.Error()}, gerr
		} else {
			// 非阻断告警（如跨校区遇到新网关 MAC）要留痕，便于排障。
			for _, w := range gres.Warnings {
				logger.Warn("安全校验告警", "detail", w)
			}
		}

		lastGatewayMAC = ni.GatewayMAC
		logger.Info("发起登录", "校区", plan.Campus, "门户", ep.Host, "协议", portalKind(plan.UseRuijie),
			"账号", logx.MaskAccount(cred.Username), "userip", ep.Extra["userip"], "ssid", ni.SSID)
		var client portal.Portal
		if plan.UseRuijie {
			rj := ruijie.New()
			rj.CaptchaDir = filepath.Join(dir, "captcha")
			client = rj
		} else {
			client = drcom.NewWithSuffixes(cur.Login.CarrierSuffixes)
		}
		res, lerr := client.Login(ctx, ep, cred)
		if lerr != nil {
			// 网络错误可能携带完整登录网址（query 含明文密码），落盘与日志前脱敏。
			lastMessage = "登录失败: " + logx.MaskURL(lerr.Error())
			logger.Warn("登录请求失败", "err", logx.MaskURL(lerr.Error()))
		} else if !res.OK {
			lastMessage = "登录失败: " + logx.MaskURL(res.Message)
			logger.Warn("网关拒绝登录", "code", res.Code, "消息", logx.MaskURL(res.Message))
		} else {
			logger.Info("网关返回登录成功", "code", res.Code)
		}
		if lerr != nil || !res.OK {
			// 立即落盘，界面不必等下一轮探针就能看到失败原因。
			_ = state.Write(dir, state.Snapshot{State: portal.StateOffline.String(), Message: lastMessage, PID: os.Getpid()})
		}
		return res, lerr
	}

	// 自动连回校园 Wi-Fi：仅当开关文件存在、且当前不属于本校区目标
	// 校园网时尝试，目标 SSID 由概览页滑条记录的优先校区决定。
	// 校区硬隔离：选本部只回连本部 SSID，选东区只回连东区 SSID。
	// 失败后 1 分钟内不重复尝试，避免 Wi-Fi 关闭时日志刷屏。
	var lastWiFiTry time.Time
	autoWiFi := func() {
		if _, err := os.Stat(filepath.Join(dir, "wifi_auto")); err != nil {
			return
		}
		ni, err := netinfo.Get()
		if err != nil {
			return
		}
		// 界面切换校区会改写 profile：每轮用磁盘上的最新配置，
		// 否则守护启动后一直按旧的优先校区回连（切了也不生效）。
		plan := session.Plan(currentProfile())
		if ssidInList(ni.SSID, plan.SSIDs) {
			return
		}
		if time.Since(lastWiFiTry) < time.Minute {
			return
		}
		lastWiFiTry = time.Now()
		for _, ssid := range plan.SSIDs {
			if err := netinfo.Connect(ssid); err != nil {
				logger.Warn("自动连接校园 Wi-Fi 失败，尝试下一个", "ssid", ssid, "err", err)
				continue
			}
			logger.Info("已请求连接校园 Wi-Fi", "ssid", ssid, "校区", plan.Campus)
			time.Sleep(3 * time.Second)
			return
		}
	}

	startPlan := session.Plan(prof)
	campus := "东区"
	if startPlan.Campus == config.CampusBenbu {
		campus = "本部"
	}
	// 打印偏好校区与真实回连目标：只打 profile/ssid 容易让人误以为仍在东区
	// （profile 是配置 ID、ssid 是结构里的东区字段，都不代表当前选择）。
	logger.Info("守护启动", "配置", prof.ID, "偏好校区", campus,
		"回连目标", strings.Join(startPlan.SSIDs, ","),
		"门户", startPlan.PortalHost)
	_ = state.Write(dir, state.Snapshot{State: "unknown", Message: "守护已启动", PID: os.Getpid()})
	exitMessage = "守护已停止"

	runErr := daemon.Run(ctx, daemon.Deps{
		Policy: prof.Policy,
		// Sleep 每 200ms 检查一次停止标记，每 5 秒刷新一次状态心跳：
		// 否则长退避（45/60 秒）期间既无法响应停止，界面也会误判"守护未运行"。
		Sleep: func(ctx context.Context, dur time.Duration) error {
			deadline := time.Now().Add(dur)
			nextBeat := time.Now().Add(5 * time.Second)
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				if state.StopRequested(dir) {
					return context.Canceled
				}
				now := time.Now()
				if !now.Before(nextBeat) {
					if snap, err := state.Read(dir); err == nil {
						snap.UpdatedAt = now
						_ = state.Write(dir, snap)
					}
					nextBeat = now.Add(5 * time.Second)
				}
				remain := time.Until(deadline)
				if remain <= 0 {
					return nil
				}
				if remain > 200*time.Millisecond {
					remain = 200 * time.Millisecond
				}
				select {
				case <-time.After(remain):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		},
		Probe: func(ctx context.Context) (portal.State, probe.Evidence, error) {
			autoWiFi()
			cur := currentProfile()
			st, ev, perr := session.ProbeOnce(ctx, cur)

			// 本部探针抖动防误判：网络差时 baidu/jxau 探针反复 timeout，
			// 状态在在线/离线间横跳。SSID 处于本部时用门户权威接口
			// getOnlineUserInfo 复核，以它为准（查询失败则维持原判定）。
			plan := session.Plan(cur)
			if st != portal.StateOnline && plan.UseRuijie && ssidInList(netinfo.SSID(), plan.SSIDs) {
				cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
				online := ruijie.CheckOnline(cctx, plan.PortalHost, plan.PortalPort)
				cancel()
				if online {
					logger.Info("探针判定不稳定，已按门户在线表复核为在线")
					st, ev, perr = portal.StateOnline, probe.Evidence{}, nil
				}
			}

			if perr != nil {
				lastMessage = perr.Error()
			} else if st == portal.StateOnline {
				lastMessage = ""
			}
			_ = state.Write(dir, state.Snapshot{State: st.String(), Message: lastMessage, PID: os.Getpid()})
			return st, ev, perr
		},
		Login: loginFn,
		Log:   logger,
		Stop:  func() bool { return state.StopRequested(dir) },
		// SSID 判离线：只认当前校区目标 SSID，3 次探不通即登录；
		// 连的是另一校区的网络则视为非目标，不代持登录。
		TargetSSIDs: func() []string { return session.Plan(currentProfile()).SSIDs },
		CurrentSSID: netinfo.SSID,
		// 门户可达预检：只探当前选中的校区门户，绝不探测另一校区。
		// 开机链路未就绪时先等 TCP 能连上门户再登录，避免发注定失败的请求。
		PortalReachable: func(ctx context.Context) bool {
			plan := session.Plan(currentProfile())
			addr := net.JoinHostPort(plan.PortalHost, strconv.Itoa(plan.PortalPort))
			cctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			conn, err := (&net.Dialer{}).DialContext(cctx, "tcp", addr)
			cancel()
			if err != nil {
				return false
			}
			_ = conn.Close()
			return true
		},
		OnVerified: func() {
			if lastGatewayMAC == "" {
				logger.Warn("网关 MAC 为空，跳过信任记录")
				return
			}
			updated, err := trust.Remember(dir, lastGatewayMAC)
			if err != nil {
				logger.Warn("记录网关 MAC 失败", "err", err)
				return
			}
			trusted = updated
			logger.Info("已记录网关 MAC", "mac", lastGatewayMAC)
		},
	})

	logger.Info("守护退出")
	return runErr
}
