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
	fmt.Printf("carrier=%s probes=%q %q\n",
		prof.Login.Carrier, prof.Network.PrimaryProbe, prof.Network.BackupProbe)

	c := probe.New()
	report := func(label, probeURL string, allowed []string) {
		state, ev, err := c.Check(ctx, probeURL, allowed)
		if err != nil {
			fmt.Printf("[%s] %s -> ERR %v\n", label, probeURL, err)
			return
		}
		fmt.Printf("[%s] %s\n  状态=%d server=%q\n  location=%s\n  正文长度=%d\n  判定=%s\n",
			label, probeURL, ev.StatusCode, ev.Server, logx.MaskURL(ev.Location), len(ev.Body), state)
	}

	report("primary", prof.Network.PrimaryProbe, prof.Network.ProbeHosts)
	report("backup", prof.Network.BackupProbe, []string{"www.baidu.com", "baidu.com"})

	state, ev, _ := c.Check(ctx, prof.Network.PrimaryProbe, prof.Network.ProbeHosts)
	if state != portal.StateOnline {
		if ep, err := endpointFromEvidence(prof, ev); err != nil {
			fmt.Printf("== 网关参数 ==\n  解析失败: %v\n", err)
		} else {
			fmt.Println("== 网关参数（已脱敏）==")
			fmt.Printf("host=%s port=%d\n", ep.Host, ep.Port)
			for _, k := range []string{"userip", "wlanacip", "usermac"} {
				fmt.Printf("%s=%s\n", k, maskValue(ep.Extra[k]))
			}
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

	ep, err := endpointFromEvidence(prof, ev)
	if err != nil {
		return err
	}

	client := drcom.New()
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

// probeOnce 先走主探针，失败时退回备探针。
func probeOnce(ctx context.Context, prof config.Profile) (portal.State, probe.Evidence, error) {
	c := probe.New()

	state, ev, err := c.Check(ctx, prof.Network.PrimaryProbe, prof.Network.ProbeHosts)
	if err == nil {
		return state, ev, nil
	}
	if prof.Network.BackupProbe == "" {
		return portal.StateUnknown, probe.Evidence{}, err
	}

	state, ev, backupErr := c.Check(ctx, prof.Network.BackupProbe, []string{"www.baidu.com", "baidu.com"})
	if backupErr != nil {
		return portal.StateUnknown, probe.Evidence{}, fmt.Errorf("主备探针均失败: %v / %v", err, backupErr)
	}
	return state, ev, nil
}

// endpointFromEvidence 从 302 的 Location 中解析网关地址与登录参数。
func endpointFromEvidence(prof config.Profile, ev probe.Evidence) (portal.Endpoint, error) {
	ep := portal.Endpoint{
		Host:      prof.Portal.Host,
		Port:      prof.Portal.Port,
		LoginPath: prof.Portal.LoginPath,
		Extra:     map[string]string{},
	}
	for k, v := range prof.Login.ExtraParams {
		ep.Extra[k] = v
	}

	if ev.Location == "" {
		return ep, fmt.Errorf("未拿到门户重定向地址，无法取得网关参数")
	}
	u, err := url.Parse(ev.Location)
	if err != nil {
		return ep, fmt.Errorf("解析重定向地址失败: %w", err)
	}
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

	if ep.Extra["userip"] == "" || ep.Extra["usermac"] == "" || ep.Extra["wlanacip"] == "" {
		return ep, fmt.Errorf("门户参数不完整（userip/usermac/wlanacip），拒绝发起登录")
	}
	return ep, nil
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
