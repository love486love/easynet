package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
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
	"github.com/love486love/easynet/internal/probe"
	"github.com/love486love/easynet/internal/session"
	"github.com/love486love/easynet/internal/state"
	"github.com/love486love/easynet/internal/trust"
)

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

	secret, err := creds.Load(dir)
	if err != nil {
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

	cred := portal.Credential{
		Username: secret.Username,
		Password: secret.Password,
		Carrier:  secret.Carrier,
	}
	if cred.Carrier == "" {
		cred.Carrier = prof.Login.Carrier
	}

	trusted, err := trust.Load(dir)
	if err != nil {
		exitMessage = "读取网关信任列表失败"
		return fmt.Errorf("读取网关信任列表失败: %w", err)
	}
	lastGatewayMAC := ""
	lastMessage := ""

	loginFn := func(ctx context.Context, ev probe.Evidence) (portal.Result, error) {
		ni, niErr := netinfo.Get()
		if niErr != nil {
			return portal.Result{Code: 6, Message: niErr.Error()}, niErr
		}

		ep, warns := session.EndpointFromEvidence(prof, ev)
		for _, w := range warns {
			logger.Warn("网关参数告警", "detail", w)
		}

		// 探针不可用（例如开机时 DNS 尚未就绪）拿不到 302 参数，
		// 用本机网卡信息补齐，避免登录请求带空 userip 被网关拒绝。
		if ep.Extra["userip"] == "" {
			ep.Extra["userip"] = ni.IPv4
		}
		if ep.Extra["usermac"] == "" {
			ep.Extra["usermac"] = ni.MAC
		}

		// 四项校验：SSID / 门户白名单 / 随机 MAC（仅告警）/ 网关 MAC 信任。
		// 信任列表为空时只记录不阻断，避免首次使用被卡死。
		if _, gerr := guard.Check(guard.Input{
			TargetSSID:   prof.Network.SSID,
			CurrentSSID:  ni.SSID,
			PortalHost:   ep.Host,
			AllowedHosts: []string{prof.Portal.Host},
			GatewayMAC:   ni.GatewayMAC,
			TrustedMACs:  trusted,
			RandomMAC:    ni.RandomMAC,
		}); gerr != nil {
			logger.Warn("安全校验未通过，放弃本次登录", "err", gerr)
			return portal.Result{Code: 6, Message: gerr.Error()}, gerr
		}

		lastGatewayMAC = ni.GatewayMAC
		logger.Info("发起登录", "门户", ep.Host, "账号", logx.MaskAccount(cred.Username),
			"userip", ep.Extra["userip"], "ssid", ni.SSID)
		client := drcom.NewWithSuffixes(prof.Login.CarrierSuffixes)
		res, lerr := client.Login(ctx, ep, cred)
		if lerr != nil {
			lastMessage = "登录失败: " + lerr.Error()
			logger.Warn("登录请求失败", "err", lerr)
		} else if !res.OK {
			lastMessage = "登录失败: " + res.Message
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

	// 自动连回校园 Wi-Fi：仅当开关文件存在、且当前不是目标 SSID 时尝试。
	// 失败后 1 分钟内不重复尝试，避免 Wi-Fi 关闭时日志刷屏。
	var lastWiFiTry time.Time
	autoWiFi := func() {
		if _, err := os.Stat(filepath.Join(dir, "wifi_auto")); err != nil {
			return
		}
		ni, err := netinfo.Get()
		if err != nil || ni.SSID == prof.Network.SSID {
			return
		}
		if time.Since(lastWiFiTry) < time.Minute {
			return
		}
		lastWiFiTry = time.Now()
		if err := netinfo.Connect(prof.Network.SSID); err != nil {
			logger.Warn("自动连接校园 Wi-Fi 失败", "err", err)
			return
		}
		logger.Info("已请求连接校园 Wi-Fi", "ssid", prof.Network.SSID)
		time.Sleep(3 * time.Second)
	}

	logger.Info("守护启动", "profile", prof.ID, "ssid", prof.Network.SSID)
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
			st, ev, perr := session.ProbeOnce(ctx, prof)
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
