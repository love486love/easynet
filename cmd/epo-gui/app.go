package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/love486love/epo-2026/internal/autostart"
	"github.com/love486love/epo-2026/internal/config"
	"github.com/love486love/epo-2026/internal/creds"
	"github.com/love486love/epo-2026/internal/netinfo"
	"github.com/love486love/epo-2026/internal/portal"
	"github.com/love486love/epo-2026/internal/portal/drcom"
	"github.com/love486love/epo-2026/internal/session"
	"github.com/love486love/epo-2026/internal/trust"
)

// App 是绑定到前端的后端对象。
type App struct {
	ctx  context.Context
	prof config.Profile
	dir  string
}

// Status 是状态接口的返回结构（字段名会原样序列化给前端）。
type Status struct {
	State      string `json:"state"`
	SSID       string `json:"ssid"`
	IPv4       string `json:"ipv4"`
	Gateway    string `json:"gateway"`
	GatewayMAC string `json:"gateway_mac"`
	MAC        string `json:"mac"`
	RandomMAC  bool   `json:"random_mac"`
	HasCred    bool   `json:"has_cred"`
	HasTrust   bool   `json:"has_trust"`
	Message    string `json:"message"`
}

// AutostartState 是自启状态。
type AutostartState struct {
	Enabled bool   `json:"enabled"`
	Value   string `json:"value"`
}

// NewApp 读取 profile 与用户目录。凭据缺失不视为错误，界面里再引导填写。
func NewApp() *App {
	prof := config.Default()
	dir, err := config.UserDir()
	if err != nil {
		dir = filepath.Join(os.TempDir(), "epo")
	}
	return &App{prof: prof, dir: dir}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// Status 采集本机网络与认证状态。
func (a *App) Status() Status {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var st Status
	if ni, err := netinfo.Get(); err == nil {
		st.SSID = ni.SSID
		st.IPv4 = ni.IPv4
		st.Gateway = ni.Gateway
		st.GatewayMAC = ni.GatewayMAC
		st.MAC = ni.MAC
		st.RandomMAC = ni.RandomMAC
	}

	state, _, err := session.ProbeOnce(ctx, a.prof)
	if err != nil {
		st.State = "unknown"
		st.Message = err.Error()
	} else {
		st.State = state.String()
	}

	if _, err := creds.Load(a.dir); err == nil {
		st.HasCred = true
	}
	if list, err := trust.Load(a.dir); err == nil && len(list) > 0 {
		st.HasTrust = true
	}
	return st
}

// Login 执行一次认证，返回可展示的结果文案。
func (a *App) Login() (string, error) {
	secret, err := creds.Load(a.dir)
	if err != nil {
		return "", fmt.Errorf("尚未保存凭据，请先填写账号密码并保存")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	state, ev, err := session.ProbeOnce(ctx, a.prof)
	if err != nil {
		return "", fmt.Errorf("探针失败: %w", err)
	}
	if state == portal.StateOnline {
		return "当前已在线，无需登录", nil
	}

	ep, warns := session.EndpointFromEvidence(a.prof, ev)
	cred := portal.Credential{Username: secret.Username, Password: secret.Password, Carrier: secret.Carrier}
	if cred.Carrier == "" {
		cred.Carrier = a.prof.Login.Carrier
	}

	client := drcom.NewWithSuffixes(a.prof.Login.CarrierSuffixes)
	res, err := client.Login(ctx, ep, cred)
	if err != nil {
		return "", err
	}
	if !res.OK {
		return "认证被拒: " + res.Message, nil
	}

	msg := "认证成功"
	if len(warns) > 0 {
		msg += "（" + strings.Join(warns, "；") + "）"
	}
	return msg, nil
}

// SaveCred 把账号密码用 DPAPI 加密保存。
func (a *App) SaveCred(username, password, carrier string) (string, error) {
	if username == "" || password == "" {
		return "", fmt.Errorf("账号和密码不能为空")
	}
	if carrier == "" {
		carrier = a.prof.Login.Carrier
	}
	if err := creds.Save(a.dir, creds.Secret{Username: username, Password: password, Carrier: carrier}); err != nil {
		return "", err
	}
	return "凭据已加密保存", nil
}

// Autostart 返回当前开机自启状态。
func (a *App) Autostart() AutostartState {
	v, _ := autostart.Value()
	return AutostartState{Enabled: v != "", Value: v}
}

// SetAutostart 开启或关闭开机自启。
func (a *App) SetAutostart(enabled bool) (string, error) {
	if !enabled {
		if err := autostart.Disable(); err != nil {
			return "", err
		}
		return "已关闭开机自启", nil
	}

	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	daemon := filepath.Join(filepath.Dir(exe), "epo-daemon.exe")
	if _, err := os.Stat(daemon); err != nil {
		return "", fmt.Errorf("缺少 epo-daemon.exe，无法设置开机自启")
	}
	if err := autostart.Enable(daemon); err != nil {
		return "", err
	}
	return "已开启开机自启", nil
}

// Logs 返回日志尾部 40 行。
func (a *App) Logs() []string {
	data, err := os.ReadFile(filepath.Join(a.dir, "logs", "epo.log"))
	if err != nil {
		return []string{}
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	return lines
}
