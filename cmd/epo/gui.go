package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/love486love/epo-2026/internal/autostart"
	"github.com/love486love/epo-2026/internal/config"
	"github.com/love486love/epo-2026/internal/creds"
	"github.com/love486love/epo-2026/internal/netinfo"
	"github.com/love486love/epo-2026/internal/portal"
	"github.com/love486love/epo-2026/internal/portal/drcom"
	"github.com/love486love/epo-2026/internal/trust"
)

//go:embed web/index.html
var webFS embed.FS

type guiServer struct {
	token string
	prof  config.Profile
	dir   string
}

// cmdGui 启动本地 Web 界面。只监听 127.0.0.1，并要求每次 API 请求带随机令牌。
func cmdGui(args []string) error {
	fs := flag.NewFlagSet("gui", flag.ExitOnError)
	profilePath := fs.String("profile", "", "profile 文件路径")
	addr := fs.String("addr", "127.0.0.1:0", "监听地址")
	noOpen := fs.Bool("no-open", false, "不自动打开浏览器")
	_ = fs.Parse(args)

	prof, err := loadProfile(*profilePath)
	if err != nil {
		return err
	}
	dir, err := config.UserDir()
	if err != nil {
		return err
	}

	token, err := newToken()
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("监听失败: %w", err)
	}

	srv := &guiServer{token: token, prof: prof, dir: dir}
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleIndex)
	mux.HandleFunc("/api/status", srv.guard(srv.handleStatus))
	mux.HandleFunc("/api/login", srv.guard(srv.handleLogin))
	mux.HandleFunc("/api/cred", srv.guard(srv.handleCred))
	mux.HandleFunc("/api/logs", srv.guard(srv.handleLogs))
	mux.HandleFunc("/api/autostart", srv.guard(srv.handleAutostart))
	mux.HandleFunc("/api/profile", srv.guard(srv.handleProfile))

	url := fmt.Sprintf("http://%s/?t=%s", ln.Addr().String(), token)
	fmt.Println("界面地址:", url)

	if !*noOpen {
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}

	httpSrv := &http.Server{Handler: mux}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func newToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// guard 校验令牌，防止其它网页跨站调用本地接口。
func (g *guiServer) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Epo-Token") != g.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (g *guiServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "界面资源缺失", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

type statusResponse struct {
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

func (g *guiServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp := statusResponse{}
	if ni, err := netinfo.Get(); err == nil {
		resp.SSID = ni.SSID
		resp.IPv4 = ni.IPv4
		resp.Gateway = ni.Gateway
		resp.GatewayMAC = ni.GatewayMAC
		resp.MAC = ni.MAC
		resp.RandomMAC = ni.RandomMAC
	}

	state, _, err := probeOnce(ctx, g.prof)
	if err != nil {
		resp.State = "unknown"
		resp.Message = err.Error()
	} else {
		resp.State = state.String()
	}

	if _, err := creds.Load(g.dir); err == nil {
		resp.HasCred = true
	}
	if list, err := trust.Load(g.dir); err == nil && len(list) > 0 {
		resp.HasTrust = true
	}

	writeJSON(w, http.StatusOK, resp)
}

func (g *guiServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	secret, err := creds.Load(g.dir)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"ok": "false", "message": "尚未保存凭据，请先在界面中填写账号密码",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()

	state, ev, err := probeOnce(ctx, g.prof)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"ok": "false", "message": "探针失败: " + err.Error()})
		return
	}
	if state == portal.StateOnline {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "message": "当前已在线，无需登录"})
		return
	}

	ep, warns := endpointFromEvidence(g.prof, ev)
	cred := portal.Credential{Username: secret.Username, Password: secret.Password, Carrier: secret.Carrier}
	if cred.Carrier == "" {
		cred.Carrier = g.prof.Login.Carrier
	}

	client := drcom.NewWithSuffixes(g.prof.Login.CarrierSuffixes)
	res, err := client.Login(ctx, ep, cred)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"ok": "false", "message": err.Error()})
		return
	}
	if !res.OK {
		writeJSON(w, http.StatusOK, map[string]string{
			"ok": "false", "message": "认证被拒: " + res.Message,
		})
		return
	}

	msg := "认证成功"
	if len(warns) > 0 {
		msg += "（" + strings.Join(warns, "；") + "）"
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "message": msg})
}

func (g *guiServer) handleCred(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Carrier  string `json:"carrier"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"ok": "false", "message": "请求格式错误"})
		return
	}
	if body.Username == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"ok": "false", "message": "账号和密码不能为空"})
		return
	}
	if body.Carrier == "" {
		body.Carrier = g.prof.Login.Carrier
	}

	secret := creds.Secret{Username: body.Username, Password: body.Password, Carrier: body.Carrier}
	if err := creds.Save(g.dir, secret); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"ok": "false", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "message": "凭据已加密保存"})
}

func (g *guiServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(g.dir, "logs", "epo.log")
	data, err := os.ReadFile(path)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"lines": []string{}})
		return
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	tail := 40
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			tail = n
		}
	}
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

func (g *guiServer) handleAutostart(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		v, _ := autostart.Value()
		writeJSON(w, http.StatusOK, map[string]any{"enabled": v != "", "value": v})
		return
	case http.MethodPost:
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"ok": "false", "message": "请求格式错误"})
			return
		}
		if body.Enabled {
			exe, err := os.Executable()
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"ok": "false", "message": err.Error()})
				return
			}
			daemon := filepath.Join(filepath.Dir(exe), "epo-daemon.exe")
			if _, err := os.Stat(daemon); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"ok": "false", "message": "缺少 epo-daemon.exe，无法设置开机自启",
				})
				return
			}
			if err := autostart.Enable(daemon); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"ok": "false", "message": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "message": "已启用开机自启"})
			return
		}
		if err := autostart.Disable(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"ok": "false", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "message": "已关闭开机自启"})
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (g *guiServer) handleProfile(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, g.prof)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
