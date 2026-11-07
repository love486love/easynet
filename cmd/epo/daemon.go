package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/love486love/epo-2026/internal/autostart"
	"github.com/love486love/epo-2026/internal/config"
	"github.com/love486love/epo-2026/internal/creds"
	"github.com/love486love/epo-2026/internal/daemon"
	"github.com/love486love/epo-2026/internal/guard"
	"github.com/love486love/epo-2026/internal/instance"
	"github.com/love486love/epo-2026/internal/logx"
	"github.com/love486love/epo-2026/internal/netinfo"
	"github.com/love486love/epo-2026/internal/portal"
	"github.com/love486love/epo-2026/internal/portal/drcom"
	"github.com/love486love/epo-2026/internal/probe"
	"github.com/love486love/epo-2026/internal/trust"
)

// cmdSaveCred 把 JSON 凭据用 DPAPI 加密后写入本地目录。
func cmdSaveCred(args []string) error {
	fs := flag.NewFlagSet("save-cred", flag.ExitOnError)
	credPath := fs.String("cred", "", "凭据文件（JSON，含 username/password/carrier）")
	_ = fs.Parse(args)

	if *credPath == "" {
		return fmt.Errorf("必须提供 -cred 凭据文件")
	}
	cred, err := readCredential(*credPath)
	if err != nil {
		return err
	}
	dir, err := config.UserDir()
	if err != nil {
		return err
	}
	secret := creds.Secret{Username: cred.Username, Password: cred.Password, Carrier: cred.Carrier}
	if err := creds.Save(dir, secret); err != nil {
		return err
	}
	fmt.Println("凭据已加密保存:", creds.Path(dir))
	return nil
}

// cmdDaemon 启动常驻守护，直到 Ctrl+C 或进程被结束。
func cmdDaemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	profilePath := fs.String("profile", "", "profile 文件路径")
	_ = fs.Parse(args)

	prof, err := loadProfile(*profilePath)
	if err != nil {
		return err
	}
	dir, err := config.UserDir()
	if err != nil {
		return err
	}

	secret, err := creds.Load(dir)
	if err != nil {
		return fmt.Errorf("读取凭据失败（请先运行 epo save-cred）: %w", err)
	}

	lock, ok, err := instance.Acquire(`Local\epo-daemon`)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("已有守护进程在运行，本次退出")
		return nil
	}
	defer lock.Release()

	logger, closeLog, err := logx.New(filepath.Join(dir, "logs"))
	if err != nil {
		return err
	}
	defer closeLog()

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
		return fmt.Errorf("读取网关信任列表失败: %w", err)
	}
	lastGatewayMAC := ""

	loginFn := func(ctx context.Context, ev probe.Evidence) (portal.Result, error) {
		ni, niErr := netinfo.Get()
		if niErr != nil {
			return portal.Result{Code: 6, Message: niErr.Error()}, niErr
		}

		ep, warns := endpointFromEvidence(prof, ev)
		for _, w := range warns {
			logger.Warn("网关参数告警", "detail", w)
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
			return portal.Result{Code: 6, Message: gerr.Error()}, gerr
		}

		lastGatewayMAC = ni.GatewayMAC
		client := drcom.NewWithSuffixes(prof.Login.CarrierSuffixes)
		return client.Login(ctx, ep, cred)
	}

	logger.Info("守护启动", "profile", prof.ID, "ssid", prof.Network.SSID)

	runErr := daemon.Run(ctx, daemon.Deps{
		Policy: prof.Policy,
		Probe:  func(ctx context.Context) (portal.State, probe.Evidence, error) { return probeOnce(ctx, prof) },
		Login:  loginFn,
		Log:    logger,
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

// cmdAutostart 管理开机自启（HKCU\Run，无需管理员权限）。
func cmdAutostart(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: epo autostart install|remove|status")
	}

	switch args[0] {
	case "install":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		target := filepath.Join(filepath.Dir(exe), "epo-daemon.exe")
		if _, statErr := os.Stat(target); statErr != nil {
			return fmt.Errorf("未找到 %s，请先构建 epo-daemon.exe（windowsgui 子系统）", target)
		}
		if err := autostart.Enable(target); err != nil {
			return err
		}
		fmt.Println("已启用开机自启:", target)
	case "remove":
		if err := autostart.Disable(); err != nil {
			return err
		}
		fmt.Println("已移除开机自启")
	case "status":
		v, err := autostart.Value()
		if err != nil {
			return err
		}
		if v == "" {
			fmt.Println("开机自启: 未启用")
		} else {
			fmt.Println("开机自启: 已启用 ->", v)
		}
	default:
		return fmt.Errorf("未知子命令 %q", args[0])
	}
	return nil
}
