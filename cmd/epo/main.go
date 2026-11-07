// epo 命令行入口。M1 提供 status / doctor / login 三个子命令。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/love486love/epo-2026/internal/app"
	"github.com/love486love/epo-2026/internal/logx"
	"github.com/love486love/epo-2026/internal/netinfo"
	"github.com/love486love/epo-2026/internal/portal"
	"github.com/love486love/epo-2026/internal/portal/drcom"
	"github.com/love486love/epo-2026/internal/probe"
	"github.com/love486love/epo-2026/internal/session"
)

const version = "0.1.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "status":
		err = cmdStatus(os.Args[2:])
	case "doctor":
		err = cmdDoctor(os.Args[2:])
	case "login":
		err = cmdLogin(os.Args[2:])
	case "daemon":
		err = cmdDaemon(os.Args[2:])
	case "save-cred":
		err = cmdSaveCred(os.Args[2:])
	case "autostart":
		err = cmdAutostart(os.Args[2:])
	case "gui":
		err = cmdGui(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println(version)
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `epo %s — 校园网门户自动认证

用法:
  epo status  [-profile 路径]   查看当前在线状态
  epo doctor  [-profile 路径]   输出诊断信息（不含凭据）
  epo login   -cred 凭据文件     执行一次认证
  epo save-cred -cred 凭据文件   用 DPAPI 加密保存凭据
  epo daemon                    常驻守护（掉线自动重连）
  epo autostart install|remove|status  开关开机自启
  epo gui                       打开本地图形界面（浏览器）
  epo version                  显示版本
`, version)
}

// ---------- status ----------

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	profilePath := fs.String("profile", "", "profile 文件路径")
	_ = fs.Parse(args)

	prof, err := app.LoadProfile(*profilePath)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	state, _, err := session.ProbeOnce(ctx, prof)
	if err != nil {
		return fmt.Errorf("探针失败: %w", err)
	}
	fmt.Printf("状态: %s\n", state)
	return nil
}

// ---------- doctor ----------

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	profilePath := fs.String("profile", "", "profile 文件路径")
	_ = fs.Parse(args)

	prof, err := app.LoadProfile(*profilePath)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	fmt.Println("== profile ==")
	fmt.Printf("id=%s name=%s ssid=%s portal=%s:%d\n",
		prof.ID, prof.DisplayName, prof.Network.SSID, prof.Portal.Host, prof.Portal.Port)
	fmt.Printf("carrier=%s 探测间隔=%ds/%ds\n",
		prof.Login.Carrier, prof.Policy.OnlineIntervalSec, prof.Policy.OfflineIntervalSec)

	fmt.Println("== 本机网络 ==")
	if ni, err := netinfo.Get(); err != nil {
		fmt.Printf("  读取失败: %v\n", err)
	} else {
		fmt.Printf("  ssid=%q ipv4=%s gateway=%s gwMAC=%s mac=%s randomMAC=%v\n",
			ni.SSID, ni.IPv4, ni.Gateway, ni.GatewayMAC, ni.MAC, ni.RandomMAC)
	}

	c := probe.New()
	firstOffline := (*probe.Evidence)(nil)
	for _, p := range prof.Network.Probes {
		state, ev, err := c.Check(ctx, p.URL, prof.Network.ProbeHosts, p.Expect)
		if err != nil {
			fmt.Printf("[probe] %s -> ERR %v\n", p.URL, err)
			continue
		}
		fmt.Printf("[probe] %s\n  status=%d server=%q\n  location=%s\n  body=%d 判定=%s\n",
			p.URL, ev.StatusCode, ev.Server, logx.MaskURL(ev.Location), len(ev.Body), state)
		if state == portal.StateOffline && firstOffline == nil {
			e := ev
			firstOffline = &e
		}
	}

	if firstOffline != nil && firstOffline.Location != "" {
		ep, warns := session.EndpointFromEvidence(prof, *firstOffline)
		fmt.Println("== 网关参数（已脱敏）==")
		fmt.Printf("host=%s port=%d\n", ep.Host, ep.Port)
		for _, k := range []string{"userip", "wlanacip", "usermac"} {
			fmt.Printf("%s=%s\n", k, maskValue(ep.Extra[k]))
		}
		for _, w := range warns {
			fmt.Printf("告警: %s\n", w)
		}
	}
	return nil
}

// ---------- login ----------

type credentialFile struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Carrier  string `json:"carrier"`
}

func cmdLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	profilePath := fs.String("profile", "", "profile 文件路径")
	credPath := fs.String("cred", "", "凭据文件路径（JSON），M1 暂不支持交互输入")
	_ = fs.Parse(args)

	if *credPath == "" {
		return fmt.Errorf("必须提供 -cred 凭据文件（M1 暂不支持交互输入，DPAPI 存储将在 M2 提供）")
	}

	prof, err := app.LoadProfile(*profilePath)
	if err != nil {
		return err
	}
	cred, err := readCredential(*credPath)
	if err != nil {
		return err
	}
	if cred.Carrier == "" {
		cred.Carrier = prof.Login.Carrier
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	state, ev, err := session.ProbeOnce(ctx, prof)
	if err != nil {
		return fmt.Errorf("探针失败: %w", err)
	}
	if state == portal.StateOnline {
		fmt.Println("当前已在线，无需登录")
		return nil
	}

	ep, warns := session.EndpointFromEvidence(prof, ev)
	for _, w := range warns {
		fmt.Println("告警:", w)
	}

	client := drcom.NewWithSuffixes(prof.Login.CarrierSuffixes)
	res, err := client.Login(ctx, ep, cred)
	if err != nil {
		return err
	}
	if !res.OK {
		return fmt.Errorf("认证被拒：%s（ret_code=%d）", res.Message, res.RetCode)
	}
	fmt.Println("认证成功")
	return nil
}

// ---------- 公共逻辑 ----------

func readCredential(path string) (portal.Credential, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return portal.Credential{}, fmt.Errorf("读取凭据失败: %w", err)
	}
	// Windows PowerShell 5.1 的 UTF8 编码会带 BOM，json 解析器不接受。
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var f credentialFile
	if err := json.Unmarshal(data, &f); err != nil {
		return portal.Credential{}, fmt.Errorf("解析凭据失败: %w", err)
	}
	if f.Username == "" || f.Password == "" {
		return portal.Credential{}, fmt.Errorf("凭据文件缺少 username 或 password")
	}
	return portal.Credential{Username: f.Username, Password: f.Password, Carrier: f.Carrier}, nil
}

func maskValue(s string) string {
	if s == "" {
		return "(空)"
	}
	if len(s) <= 8 {
		return s
	}
	return s[:4] + "…"
}
