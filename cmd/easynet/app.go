package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/love486love/easynet/internal/autostart"
	"github.com/love486love/easynet/internal/config"
	"github.com/love486love/easynet/internal/creds"
	"github.com/love486love/easynet/internal/netinfo"
	"github.com/love486love/easynet/internal/portal"
	"github.com/love486love/easynet/internal/portal/drcom"
	"github.com/love486love/easynet/internal/session"
	"github.com/love486love/easynet/internal/state"
	"github.com/love486love/easynet/internal/trust"
	"github.com/wailsapp/wails/v2/pkg/runtime"
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
	Current  bool   `json:"current"`
}

// NewApp 读取 profile 与用户目录。凭据缺失不视为错误，界面里再引导填写。
func NewApp() *App {
	prof := config.Default()
	dir, err := config.UserDir()
	if err != nil {
		dir = filepath.Join(os.TempDir(), "easynet")
	}
	return &App{prof: prof, dir: dir}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// 守护开关默认开启：界面就绪后自动补上，用户不必每次手动点一次。
	go func() {
		time.Sleep(1500 * time.Millisecond)
		_, _ = a.EnsureDaemon()
	}()
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

	if store, err := creds.LoadStore(a.dir); err == nil && len(store.Accounts) > 0 {
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

// SaveCred 新增或更新账号（DPAPI 加密），并把它设为当前账号。
func (a *App) SaveCred(username, password, carrier string) (string, error) {
	if username == "" || password == "" {
		return "", fmt.Errorf("账号和密码不能为空")
	}
	if carrier == "" {
		carrier = a.prof.Login.Carrier
	}

	store, loadErr := creds.LoadStore(a.dir)
	if loadErr == nil {
		for _, acc := range store.Accounts {
			if acc.Username == username && acc.Password == password && acc.Carrier == carrier {
				if store.Current != username {
					if _, err := creds.SelectAccount(a.dir, username); err != nil {
						return "", err
					}
					return "已切换到该账号（重启守护后生效）", nil
				}
				return "该账号已保存，无需重复添加", nil
			}
		}
	}

	if _, err := creds.SaveAccount(a.dir, creds.Secret{Username: username, Password: password, Carrier: carrier}); err != nil {
		return "", err
	}
	return "账号已保存（重启守护后生效）", nil
}

// Accounts 返回全部已保存账号；密码不会返回给前端。
func (a *App) Accounts() []AccountInfo {
	store, err := creds.LoadStore(a.dir)
	if err != nil {
		return []AccountInfo{}
	}
	out := make([]AccountInfo, 0, len(store.Accounts))
	for _, acc := range store.Accounts {
		out = append(out, AccountInfo{Username: acc.Username, Carrier: acc.Carrier, Current: acc.Username == store.Current})
	}
	return out
}

// SelectAccount 切换当前账号；守护若在运行则用新账号重启。
func (a *App) SelectAccount(username string) (string, error) {
	if _, err := creds.SelectAccount(a.dir, username); err != nil {
		return "", err
	}
	if a.Daemon().Running {
		_ = state.RequestStop(a.dir)
		go a.restartDaemon()
		return "已切换账号，守护正在用新账号重启", nil
	}
	return "已切换账号", nil
}

// DeleteAccount 删除指定账号；删除当前账号且守护在运行时重启守护。
func (a *App) DeleteAccount(username string) (string, error) {
	if username == "" {
		return "", fmt.Errorf("未指定账号")
	}
	if _, err := creds.DeleteAccount(a.dir, username); err != nil {
		return "", err
	}
	if a.Daemon().Running {
		_ = state.RequestStop(a.dir)
		go a.restartDaemon()
		return "账号已删除，守护正在重启", nil
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
	if err := autostart.Enable(exe, daemonArg); err != nil {
		return "", err
	}
	return "已开启开机自启", nil
}

// Logs 返回日志尾部 40 行。
func (a *App) Logs() []string {
	data, err := os.ReadFile(filepath.Join(config.LogDir(), "easynet.log"))
	if err != nil {
		return []string{}
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	return lines
}

// TutorialDone 报告新手引导是否已经播放过（标记文件在用户目录）。
func (a *App) TutorialDone() bool {
	_, err := os.Stat(filepath.Join(a.dir, "tutorial_done"))
	return err == nil
}

// MarkTutorialDone 记录新手引导已播放，之后启动不再自动弹出。
func (a *App) MarkTutorialDone() error {
	return os.WriteFile(filepath.Join(a.dir, "tutorial_done"), []byte("1"), 0o644)
}

// SaveLogAs 弹出系统保存对话框并导出完整日志。
// 返回空字符串表示用户取消。
func (a *App) SaveLogAs() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("界面尚未就绪，请稍后重试")
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出运行日志",
		DefaultFilename: "easynet-log-" + time.Now().Format("2006-01-02-15-04-05") + ".txt",
		Filters:         []runtime.FileFilter{{DisplayName: "文本文件 (*.txt)", Pattern: "*.txt"}},
	})
	if err != nil {
		return "", fmt.Errorf("打开保存对话框失败: %w", err)
	}
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(filepath.Join(config.LogDir(), "easynet.log"))
	if err != nil {
		return "", fmt.Errorf("读取日志失败: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	return "日志已导出: " + path, nil
}

// OpenLogsDir 用资源管理器打开日志目录。
func (a *App) OpenLogsDir() (string, error) {
	dir := config.LogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := exec.Command("explorer.exe", dir).Start(); err != nil {
		return "", fmt.Errorf("打开目录失败: %w", err)
	}
	return "已打开日志目录", nil
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

// StartDaemon 拉起本程序的守护模式。守护是独立进程，关闭界面后仍在运行。
func (a *App) StartDaemon() (string, error) {
	if a.Daemon().Running {
		return "守护已在运行", nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if err := state.ClearStop(a.dir); err != nil {
		return "", err
	}
	cmd := exec.Command(exe, daemonArg)
	cmd.Dir = filepath.Dir(exe)
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

// daemonOffFile 是「守护开关已关闭」的标记文件；不存在时视为开启，因此默认就是开着。
const daemonOffFile = "daemon_off"

// DaemonEnabled 报告守护开关是否开启。默认开启。
func (a *App) DaemonEnabled() bool {
	_, err := os.Stat(filepath.Join(a.dir, daemonOffFile))
	return os.IsNotExist(err)
}

// SetDaemonEnabled 持久化守护开关，并按需启停守护进程。
// 开启状态会一直保留：关掉界面、甚至从托盘退出后，重新打开界面都会自动恢复守护。
func (a *App) SetDaemonEnabled(enabled bool) (string, error) {
	marker := filepath.Join(a.dir, daemonOffFile)
	if enabled {
		if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if a.Daemon().Running {
			return "守护已在运行", nil
		}
		return a.StartDaemon()
	}
	if err := os.WriteFile(marker, []byte("1"), 0o644); err != nil {
		return "", err
	}
	return a.StopDaemon()
}

// EnsureDaemon 在开关开启且已有账号时拉起守护；不满足条件时静默跳过。
func (a *App) EnsureDaemon() (string, error) {
	if !a.DaemonEnabled() {
		return "", nil
	}
	if a.Daemon().Running {
		return "", nil
	}
	if _, err := creds.Load(a.dir); err != nil {
		return "", nil
	}
	return a.StartDaemon()
}

// restartDaemon 等旧守护退出后，按开关决定是否用新账号重新拉起。
func (a *App) restartDaemon() {
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if !a.Daemon().Running {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	_ = state.ClearStop(a.dir)
	_, _ = a.EnsureDaemon()
}
