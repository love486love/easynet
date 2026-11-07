// epo 命令行入口。M1 提供 status / doctor / login 三个子命令。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/love486love/epo-2026/internal/config"
	"github.com/love486love/epo-2026/internal/logx"
	"github.com/love486love/epo-2026/internal/netinfo"
	"github.com/love486love/epo-2026/internal/portal"
	"github.com/love486love/epo-2026/internal/portal/drcom"
	"github.com/love486love/epo-2026/internal/probe"
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
  epo version                  显示版本
`, version)
}

// ---------- status ----------

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	profilePath := fs.String("profile", "", "profile 文件路径")
	_ = fs.Parse(args)

	prof, err := loadProfile(*profilePath)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	state, _, err := probeOnce(ctx, prof)
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

	prof, err := loadProfile(*profilePath)
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
		fmt.Printf("  ssid=%q ipv4=%s gateway=%s mac=%s randomMAC=%v\n",
			ni.SSID, ni.IPv4, ni.Gateway, ni.MAC, ni.RandomMAC)
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
		ep, warns := endpointFromEvidence(prof, *firstOffline)
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

	prof, err := loadProfile(*profilePath)
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

	state, ev, err := probeOnce(ctx, prof)
	if err != nil {
		return fmt.Errorf("探针失败: %w", err)
	}
	if state == portal.StateOnline {
		fmt.Println("当前已在线，无需登录")
		return nil
	}

	ep, warns := endpointFromEvidence(prof, ev)
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

func loadProfile(path string) (config.Profile, error) {
	if path != "" {
		return config.Load(path)
	}
	dir, err := config.UserDir()
	if err != nil {
		return config.Default(), nil
	}
	return config.Load(filepath.Join(dir, "config.json"))
}

func readCredential(path string) (portal.Credential, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return portal.Credential{}, fmt.Errorf("读取凭据失败: %w", err)
	}
	var f credentialFile
	if err := json.Unmarshal(data, &f); err != nil {
		return portal.Credential{}, fmt.Errorf("解析凭据失败: %w", err)
	}
	if f.Username == "" || f.Password == "" {
		return portal.Credential{}, fmt.Errorf("凭据文件缺少 username 或 password")
	}
	return portal.Credential{Username: f.Username, Password: f.Password, Carrier: f.Carrier}, nil
}

// probeOnce 逐个探针判定：任意一个在线即在线。
// 全部失败才视为离线，避免单站点故障造成误判。
func probeOnce(ctx context.Context, prof config.Profile) (portal.State, probe.Evidence, error) {
	c := probe.New()

	var firstOffline *probe.Evidence
	var lastErr error

	for _, p := range prof.Network.Probes {
		state, ev, err := c.Check(ctx, p.URL, prof.Network.ProbeHosts, p.Expect)
		if err != nil {
			lastErr = err
			continue
		}
		if state == portal.StateOnline {
			return state, ev, nil
		}
		if state == portal.StateOffline && firstOffline == nil {
			e := ev
			firstOffline = &e
		}
	}

	if firstOffline != nil {
		return portal.StateOffline, *firstOffline, nil
	}
	if lastErr != nil {
		return portal.StateUnknown, probe.Evidence{}, lastErr
	}
	return portal.StateUnknown, probe.Evidence{}, nil
}

// endpointFromEvidence 从 302 的 Location 中解析网关地址与登录参数。
// 参数缺失只告警不阻断：同类项目实测这些字段并非认证必填项。
func endpointFromEvidence(prof config.Profile, ev probe.Evidence) (portal.Endpoint, []string) {
	ep := portal.Endpoint{
		Host:      prof.Portal.Host,
		Port:      prof.Portal.Port,
		LoginPath: prof.Portal.LoginPath,
		Extra:     map[string]string{},
	}
	for k, v := range prof.Login.ExtraParams {
		ep.Extra[k] = v
	}

	var warns []string

	if ev.Location == "" {
		warns = append(warns, "未拿到门户重定向地址，网关参数可能缺失")
	} else if u, err := url.Parse(ev.Location); err == nil {
		if h := u.Hostname(); h != "" {
			ep.Host = h
		}
		if ps := u.Port(); ps != "" {
			if n, err := strconv.Atoi(ps); err == nil {
				ep.Port = n
			}
		}
		q := u.Query()
		ep.Extra["userip"] = q.Get("userip")
		ep.Extra["usermac"] = q.Get("usermac")
		ep.Extra["wlanacip"] = q.Get("wlanacip")
	}

	for _, k := range []string{"userip", "usermac", "wlanacip"} {
		if ep.Extra[k] == "" {
			warns = append(warns, k+" 为空，仍尝试登录")
		}
	}
	return ep, warns
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
