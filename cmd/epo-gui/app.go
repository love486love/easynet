package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	"github.com/love486love/epo-2026/internal/state"
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
	Detail     string `json:"detail"`
}

// AutostartState 是自启状态。
type AutostartState struct {
	Enabled bool   `json:"enabled"`
	Value   string `json:"value"`
}

// DaemonInfo 是守护进程的当前状态（由 state.json 快照推断）。
type DaemonInfo struct {
	Running   bool      `json:"running"`
	State     string    `json:"state"`
	Message   string    `json:"message"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AccountInfo 是已保存账号的可展示信息（不含密码）。
type AccountInfo struct {
	Username string `json:"username"`
	Carrier  string `json:"carrier"`
	Saved    bool   `json:"saved"`
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
		st.Message = networkHint(err, st.SSID)
		st.Detail = err.Error()
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
		ni, _ := netinfo.Get()
		return "", fmt.Errorf("%s", networkHint(err, ni.SSID))
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
	return "凭据已加密保存（重启守护后生效）", nil
}

// Account 返回已保存的账号信息；密码不会返回给前端。
func (a *App) Account() AccountInfo {
	s, err := creds.Load(a.dir)
	if err != nil {
		return AccountInfo{}
	}
	return AccountInfo{Username: s.Username, Carrier: s.Carrier, Saved: true}
}

// DeleteAccount 删除本地加密凭据；守护若在运行则同时请求停止，
// 否则它会继续使用内存里的旧凭据。
func (a *App) DeleteAccount() (string, error) {
	if err := creds.Delete(a.dir); err != nil {
		return "", err
	}
	if a.Daemon().Running {
		_ = state.RequestStop(a.dir)
		return "账号已删除，守护已停止", nil
	}
	return "账号已删除", nil
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

// networkHint 把探针的底层错误翻译成普通用户能理解的提示。
// 原始错误仍由 Detail 字段展示，供排查使用。
func networkHint(err error, ssid string) string {
	msg := err.Error()
	switch {
	case ssid == "":
		return "请先在系统里打开 Wi-Fi 并选择校园网"
	case strings.Contains(msg, "no such host"):
		return "网络未连通：域名解析失败，请检查 Wi-Fi 是否正常"
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline exceeded"):
		return "网络无响应，请稍后重试"
	case strings.Contains(msg, "connection refused"):
		return "网络被拦截，请尝试打开学校门户完成认证"
	default:
		return "暂时无法判断网络状态，请稍后点刷新"
	}
}

// Daemon 返回后台守护的运行状态。超过 20 秒没有快照更新视为未运行。
func (a *App) Daemon() DaemonInfo {
	snap, err := state.Read(a.dir)
	if err != nil {
		return DaemonInfo{}
	}
	return DaemonInfo{
		Running:   snap.State != "stopped" && time.Since(snap.UpdatedAt) <= 20*time.Second,
		State:     snap.State,
		Message:   snap.Message,
		UpdatedAt: snap.UpdatedAt,
	}
}

// StartDaemon 拉起 epo-daemon.exe。守护是独立进程，关闭界面后仍在运行。
func (a *App) StartDaemon() (string, error) {
	if a.Daemon().Running {
		return "守护已在运行", nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(exe), "epo-daemon.exe")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("未找到 epo-daemon.exe，请与 epo-gui.exe 放在同一目录")
	}
	if err := state.ClearStop(a.dir); err != nil {
		return "", err
	}
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("启动守护失败: %w", err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		if a.Daemon().Running {
			return "守护已启动", nil
		}
	}
	return "已启动，但尚未收到状态快照，请查看运行日志", nil
}

// StopDaemon 写停止标记让守护优雅退出，避免跨进程强杀。
func (a *App) StopDaemon() (string, error) {
	if !a.Daemon().Running {
		return "守护未在运行", nil
	}
	if err := state.RequestStop(a.dir); err != nil {
		return "", err
	}
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		if !a.Daemon().Running {
			return "守护已停止", nil
		}
	}
	return "已发送停止请求，守护将在下一轮检查后退出", nil
}
