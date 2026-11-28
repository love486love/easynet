package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	releaseAPI = "https://api.github.com/repos/love486love/easynet/releases/latest"
	userAgent  = "easynet"
	// chunks 是分块下载的并发数。部分校园网出口对单连接限速在十几 KB/s，
	// 多连接可以叠加带宽；并发再调高反而会被出口限制，8 路实测最稳。
	chunks = 8
	// maxAttempts 是接口请求的最大尝试次数。校园网出口会偶发 504，
	// 有限次重试后仍失败就直接报错，不会无限循环。
	maxAttempts = 4
	// swapRenameRetries 是单步改名的重试次数。安全软件常在 exe 落盘的瞬间
	// 扫描并短暂锁文件，改名会因此瞬间失败，重试几次即可绕过。
	swapRenameRetries = 4
	// updateDirectFile 是「更新时直连」开关的标记文件；不存在即走系统代理。
	updateDirectFile = "update_direct"
)

// updateDirect 为 true 时更新请求直连，不使用系统代理。
var updateDirect atomic.Bool

// updateClient 是更新专用客户端：只切换是否走代理，其余沿用默认传输。
// 校园网里常见「系统代理指向未启动的本地代理」，直连能绕开这种失败。
var updateClient = &http.Client{Transport: newUpdateTransport()}

func newUpdateTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = func(r *http.Request) (*url.URL, error) {
		if updateDirect.Load() {
			return nil, nil
		}
		return http.ProxyFromEnvironment(r)
	}
	return t
}

// initUpdateProxy 按磁盘设置初始化是否直连。
func initUpdateProxy(dir string) {
	if _, err := os.Stat(filepath.Join(dir, updateDirectFile)); err == nil {
		updateDirect.Store(true)
	}
}

// UpdateDirect 报告更新请求是否直连（不走系统代理）。默认关闭。
func (a *App) UpdateDirect() bool {
	_, err := os.Stat(filepath.Join(a.dir, updateDirectFile))
	return err == nil
}

// SetUpdateDirect 持久化更新直连开关，下一次请求立即生效。
func (a *App) SetUpdateDirect(enabled bool) (string, error) {
	path := filepath.Join(a.dir, updateDirectFile)
	if enabled {
		if err := os.WriteFile(path, []byte("1"), 0o644); err != nil {
			return "", err
		}
		updateDirect.Store(true)
		return "已开启：检查更新将直连 GitHub", nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	updateDirect.Store(false)
	return "已关闭：检查更新按系统代理设置连接", nil
}

// assetRef 记录一个 Release 资产的两个下载入口：
// API 入口会 302 到真实文件域，能绕开被网关拦截的 github.com 直链。
type assetRef struct {
	API     string
	Browser string
	Size    int64
	Name    string
}

// progressEmitter 把下载进度推给界面：百分比与速度（KB/s）。
type progressEmitter func(pct int, speedKB int64)

// progressReporter 汇总并发分片的下载字节数，并按固定间隔节流上报。
type progressReporter struct {
	total int64
	done  int64
	mu    sync.Mutex
	start time.Time
	last  time.Time
	emit  progressEmitter
}

func newProgressReporter(total int64, emit progressEmitter) *progressReporter {
	now := time.Now()
	return &progressReporter{total: total, start: now, last: now, emit: emit}
}

func (p *progressReporter) add(n int64) {
	if p == nil || n <= 0 {
		return
	}
	p.mu.Lock()
	p.done += n
	now := time.Now()
	if now.Sub(p.last) < 250*time.Millisecond {
		p.mu.Unlock()
		return
	}
	p.last = now
	done, total := p.done, p.total
	elapsed := now.Sub(p.start).Seconds()
	p.mu.Unlock()

	if total <= 0 || elapsed <= 0 {
		return
	}
	pct := int(done * 100 / total)
	// 分片重试会重复累计字节，这里最多报到 99，100 只由 finish 发出。
	if pct > 99 {
		pct = 99
	}
	if p.emit != nil {
		p.emit(pct, int64(float64(done)/1024/elapsed))
	}
}

func (p *progressReporter) finish() {
	if p == nil || p.total <= 0 || p.emit == nil {
		return
	}
	p.emit(100, 0)
}

// progressWriter 在写入的同时累加已下载字节。
type progressWriter struct {
	w io.Writer
	p *progressReporter
}

func (pw *progressWriter) Write(b []byte) (int, error) {
	n, err := pw.w.Write(b)
	pw.p.add(int64(n))
	return n, err
}

// updateEmitter 把进度通过 Wails 事件推给前端。
func (a *App) updateEmitter() progressEmitter {
	return func(pct int, speedKB int64) {
		if a.ctx == nil {
			return
		}
		runtime.EventsEmit(a.ctx, "update-progress", map[string]any{
			"pct":   pct,
			"speed": speedKB,
		})
	}
}

// UpdateInfo 是一次更新检查的结果。
type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	HasUpdate bool   `json:"has_update"`
	URL       string `json:"url"`
	Body      string `json:"body"`
	CanAuto   bool   `json:"can_auto"`

	exe   assetRef
	sum   assetRef
	setup assetRef
}

// CheckUpdate 查询 GitHub 上的最新 Release，并与当前版本比较。
func (a *App) CheckUpdate() (UpdateInfo, error) {
	log := a.logger()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := requestWithRetry(ctx, updateClient, releaseAPI, map[string]string{
		"Accept":     "application/vnd.github+json",
		"User-Agent": userAgent,
	}, func(r *http.Response) bool { return r.StatusCode == http.StatusOK })
	if err != nil {
		// 检查更新失败是常见情况（没网、代理未启动），按告警记录即可。
		log.Warn("检查更新失败", "当前版本", Version, "直连", updateDirect.Load(), "err", err)
		return UpdateInfo{}, fmt.Errorf("检查更新失败：%s", updateErrHint(err))
	}
	defer resp.Body.Close()

	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
		Assets  []struct {
			Name    string `json:"name"`
			URL     string `json:"url"`
			Browser string `json:"browser_download_url"`
			Size    int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		log.Error("检查更新失败", "err", err)
		return UpdateInfo{}, fmt.Errorf("解析更新信息失败: %w", err)
	}

	info := UpdateInfo{
		Current: Version,
		Latest:  strings.TrimPrefix(rel.TagName, "v"),
		URL:     rel.HTMLURL,
		Body:    rel.Body,
	}
	for _, as := range rel.Assets {
		ref := assetRef{API: as.URL, Browser: as.Browser, Size: as.Size, Name: as.Name}
		switch {
		case as.Name == "easynet.exe":
			info.exe = ref
		case as.Name == "checksums.txt":
			info.sum = ref
		case strings.HasPrefix(as.Name, "easynet-setup-") && strings.HasSuffix(as.Name, ".exe"):
			info.setup = ref
		}
	}
	info.HasUpdate = versionLess(info.Current, info.Latest)
	info.CanAuto = (info.exe.API != "" && info.sum.API != "") || info.setup.API != ""
	log.Info("检查更新", "当前版本", info.Current, "最新版本", info.Latest,
		"发现新版本", info.HasUpdate, "可自动更新", info.CanAuto)
	return info, nil
}

// updateErrHint 把检查更新失败翻译成可操作的原因。
func updateErrHint(err error) string {
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "proxy"):
		return "代理未启动或不可用，可在设置中开启「更新时直连」后重试"
	case strings.Contains(s, "timeout") || strings.Contains(s, "deadline"):
		return "连接 GitHub 超时，请稍后重试；若开了代理，可尝试「更新时直连」"
	default:
		return "无法连接 GitHub，请稍后重试；若代理未启动，可在设置中开启「更新时直连」"
	}
}

// ApplyUpdate 下载并安装最新版本。绿色版原地替换 exe，安装版运行新版安装包。
// 成功后当前进程会退出，新版本自动启动。
func (a *App) ApplyUpdate() (string, error) {
	log := a.logger()
	info, err := a.CheckUpdate()
	if err != nil {
		return "", err
	}
	if !info.HasUpdate {
		return "已是最新版本", nil
	}
	if !info.CanAuto {
		log.Warn("当前版本不支持自动更新", "目标版本", info.Latest)
		return "", fmt.Errorf("当前发布缺少自动更新所需的文件，请到 Release 页面手动下载")
	}

	mode := "绿色版"
	if installedMode() {
		mode = "安装版"
	}
	log.Info("开始更新", "模式", mode, "当前版本", info.Current, "目标版本", info.Latest)

	emit := a.updateEmitter()
	if installedMode() {
		return applyInstaller(log, info, emit)
	}
	return a.applyGreen(info, emit)
}

// applyGreen 适用于绿色版：下载裸 exe、校验、原地替换。
func (a *App) applyGreen(info UpdateInfo, emit progressEmitter) (string, error) {
	log := a.logger()
	if info.exe.API == "" || info.sum.API == "" {
		return "", fmt.Errorf("当前发布缺少 exe 或校验文件，请手动下载更新")
	}
	stopDaemonForUpdate()

	sumData, err := downloadAsset(log, info.sum, nil)
	if err != nil {
		return "", fmt.Errorf("下载校验文件失败：%v", err)
	}
	want, err := parseChecksum(sumData, "easynet.exe")
	if err != nil {
		log.Error("解析校验文件失败", "err", err)
		return "", err
	}
	log.Info("已获取校验值", "文件", "easynet.exe", "SHA256", want)

	data, err := downloadAsset(log, info.exe, emit)
	if err != nil {
		log.Error("下载新版本失败", "err", err)
		return "", fmt.Errorf("下载新版本失败：%v", err)
	}

	got := sha256.Sum256(data)
	if !strings.EqualFold(fmt.Sprintf("%x", got), want) {
		log.Error("下载文件校验失败，已取消更新", "期望", want, "实际", fmt.Sprintf("%x", got))
		return "", fmt.Errorf("文件校验失败，已取消更新，当前版本未改动")
	}
	log.Info("校验通过", "SHA256", want)

	helper, err := stageGreen(data)
	if err != nil {
		return "", err
	}
	log.Info("新版本已就绪，等待进程退出后替换", "脚本", helper)
	launchHelperAfterExit(helper)
	return "更新文件已就绪，正在退出并替换…", nil
}

// stageGreen 把新版本写成同目录的 .new 文件，并生成退出后替换的脚本。
// 运行中的 exe 不能被同名覆盖，先落盘再退出替换，任何一步失败旧版本都完好。
func stageGreen(data []byte) (string, error) {
	target, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("获取程序路径失败: %v", err)
	}
	neu, _ := updatePaths(target)
	if err := os.Remove(neu); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("清理上次更新残留失败: %v", err)
	}
	if err := os.WriteFile(neu, data, 0o755); err != nil {
		return "", fmt.Errorf("写入新版本失败（当前版本未改动）: %v", err)
	}
	if st, err := os.Stat(neu); err != nil || st.Size() != int64(len(data)) {
		return "", fmt.Errorf("新版本写入不完整（当前版本未改动）")
	}
	helper := writeUpdateHelper(filepath.Dir(target))
	if helper == "" {
		return "", fmt.Errorf("无法生成更新脚本（当前版本未改动），请手动下载并用 %s 替换 easynet.exe",
			filepath.Base(neu))
	}
	return helper, nil
}

// isAccessDenied 判断错误是否为安全软件/权限拦截（Windows: Access is denied）。
func isAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrPermission) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "Access is denied") ||
		strings.Contains(msg, "拒绝访问")
}

// writeUpdateHelper 生成退出后替换的批处理脚本，返回其路径（失败返回空）。
// 脚本先等旧进程退出，再把 .new 换成主程序；任何一步失败都恢复旧版本。
func writeUpdateHelper(dir string) string {
	script := `@echo off
chcp 65001 >nul
cd /d "%~dp0"
rem 等待旧进程（含守护）退出，最多 60 秒
for /l %%i in (1,1,60) do (
  tasklist /fi "imagename eq easynet.exe" 2>nul | find /i "easynet.exe" >nul
  if errorlevel 1 goto replace
  ping -n 2 127.0.0.1 >nul
)
:replace
if not exist ".easynet.exe.new" goto restore
if not exist "easynet.exe" if exist ".easynet.exe.old.probe" ren ".easynet.exe.old.probe" "easynet.exe"
if not exist "easynet.exe" if exist ".easynet.exe.old" ren ".easynet.exe.old" "easynet.exe"
if exist "easynet.exe" (
  if exist ".easynet.exe.old" del /f /q ".easynet.exe.old" >nul 2>nul
  ren "easynet.exe" ".easynet.exe.old"
)
ren ".easynet.exe.new" "easynet.exe"
if exist "easynet.exe" (
  del /f /q ".easynet.exe.old" >nul 2>nul
  start "" "easynet.exe"
  exit /b 0
)
:restore
if not exist "easynet.exe" if exist ".easynet.exe.old" ren ".easynet.exe.old" "easynet.exe"
if not exist "easynet.exe" if exist ".easynet.exe.old.probe" ren ".easynet.exe.old.probe" "easynet.exe"
if exist "easynet.exe" start "" "easynet.exe"
exit /b 1
`
	path := filepath.Join(dir, "update-helper.bat")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		return ""
	}
	return path
}

// cleanupUpdateResidue 删除上次成功升级留下的 .easynet.exe.old。
// Windows 无法覆盖正在运行的 exe，selfupdate 只能先把旧文件改名、再落盘新文件；
// 重启后旧进程已退出，这个备份不会再被用到，在启动最初清掉即可。
func cleanupUpdateResidue() {
	target, err := os.Executable()
	if err != nil {
		return
	}
	cleanupUpdateResidueAt(target)
}

// cleanupUpdateResidueAt 清理 target 对应的旧版本备份，返回是否已删除。
// 目标文件缺失时不动手：那说明上次替换失败，备份是唯一的可执行文件。
func cleanupUpdateResidueAt(target string) bool {
	// 目标缺失说明上次更新中途失败：先恢复，否则程序下次启动不了。
	if !fileExists(target) && !healUpdateState(nil, target) {
		return false
	}
	_, old := updatePaths(target)
	removed := false
	for _, p := range []string{old, old + ".probe"} {
		if !fileExists(p) {
			continue
		}
		if err := os.Remove(p); err == nil {
			removed = true
		} else {
			// 安全软件扫描常在这个瞬间占用旧文件，隔几秒再试一次，不阻塞启动。
			time.AfterFunc(3*time.Second, func() { _ = os.Remove(p) })
		}
	}
	return removed
}

// updatePaths 返回与 target 同目录的新文件、备份文件路径（与 selfupdate 约定一致）。
func updatePaths(target string) (neu, old string) {
	dir, base := filepath.Dir(target), filepath.Base(target)
	return filepath.Join(dir, "."+base+".new"), filepath.Join(dir, "."+base+".old")
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// updateState 描述目标、新、旧三个文件的存在性，失败时写进日志便于事后定位。
func updateState(target string) string {
	neu, old := updatePaths(target)
	return fmt.Sprintf("目标文件存在=%v 新文件存在=%v 旧备份存在=%v", fileExists(target), fileExists(neu), fileExists(old))
}

// healUpdateState 修复上一次替换失败留下的“目标文件缺失”状态：
// 备份还在 .old，把它改回目标名，让程序至少能用旧版本正常启动。
func healUpdateState(log *slog.Logger, target string) bool {
	if fileExists(target) {
		return false
	}
	_, old := updatePaths(target)
	// 旧版本可能留在 .old（替换中失败），也可能留在 .old.probe（改名预检失败）。
	for _, src := range []string{old, old + ".probe"} {
		if !fileExists(src) {
			continue
		}
		if log != nil {
			log.Warn("检测到更新残留（目标文件缺失），恢复旧版本", "来源", src, "状态", updateState(target))
		}
		if err := renameWithRetry(src, target); err != nil {
			if log != nil {
				if isAccessDenied(err) {
					log.Error("恢复旧版本被安全软件拦截——程序文件当前缺失，下次将无法启动！",
						"err", err, "来源", src, "目标", target,
						"指引", "请手动将 "+src+" 重命名为 "+target+"，或双击同目录 update-helper.bat")
				} else {
					log.Error("恢复旧版本文件失败", "err", err, "来源", src, "目标", target)
				}
			}
			continue
		}
		if log != nil {
			log.Info("已恢复旧版本文件", "来源", src, "target", target)
		}
		return true
	}
	return false
}

// renameWithRetry 改名带有限次重试：安全软件的扫描锁窗口通常只有几百毫秒。
func renameWithRetry(from, to string) error {
	var err error
	for i := 0; i < swapRenameRetries; i++ {
		if i > 0 {
			time.Sleep(400 * time.Millisecond)
		}
		if err = os.Rename(from, to); err == nil {
			return nil
		}
	}
	return err
}

// downloadAsset 下载一个资产。先通过资产 API 换取带签名的真实地址，
// 再按文件大小选择单连接或并发分块。
func downloadAsset(log *slog.Logger, ref assetRef, emit progressEmitter) ([]byte, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	realURL, err := resolveAsset(ctx, ref.API)
	if err != nil {
		// API 不可用时退回直链；境外网络下直链通常更快。
		if ref.Browser == "" {
			if log != nil {
				log.Error("获取下载地址失败", "文件", ref.Name, "err", err)
			}
			return nil, err
		}
		if log != nil {
			log.Warn("API 下载地址不可用，改用直链", "文件", ref.Name, "err", err)
		}
		realURL = ref.Browser
		ref.Size = 0
	} else if log != nil {
		log.Info("已获取下载地址", "文件", ref.Name, "host", urlHost(realURL))
	}

	rep := newProgressReporter(ref.Size, emit)
	var data []byte
	if ref.Size >= 1<<20 {
		if log != nil {
			log.Info("开始下载", "文件", ref.Name, "方式", "并发分块", "字节", ref.Size)
		}
		data, err = downloadChunked(ctx, realURL, ref.Size, rep)
	} else {
		if log != nil {
			log.Info("开始下载", "文件", ref.Name, "方式", "单连接", "字节", ref.Size)
		}
		data, err = downloadSimple(ctx, realURL, rep)
		if err == nil && ref.Size > 0 {
			rep.finish()
		}
	}

	if log != nil {
		if err != nil {
			log.Error("下载失败", "文件", ref.Name, "耗时", time.Since(start).Round(time.Second).String(), "err", err)
		} else {
			elapsed := time.Since(start)
			speed := int64(0)
			if sec := elapsed.Seconds(); sec > 0 {
				speed = int64(float64(len(data)) / 1024 / sec)
			}
			log.Info("下载完成", "文件", ref.Name, "字节", len(data),
				"耗时", elapsed.Round(time.Second).String(), "平均速度", fmt.Sprintf("%d KB/s", speed))
		}
	}
	return data, err
}

// urlHost 只取地址的 host 部分：签名地址的 query 里带临时凭据，不进日志。
func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

// resolveAsset 用资产 API 换取 302 的真实下载地址。
func resolveAsset(ctx context.Context, apiURL string) (string, error) {
	if apiURL == "" {
		return "", errors.New("没有可用的下载地址")
	}

	client := &http.Client{
		Transport:     updateClient.Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := requestWithRetry(ctx, client, apiURL, map[string]string{
		"Accept":     "application/octet-stream",
		"User-Agent": userAgent,
	}, func(r *http.Response) bool {
		return r.StatusCode == http.StatusFound || r.StatusCode == http.StatusSeeOther ||
			r.StatusCode == http.StatusTemporaryRedirect
	})
	if err != nil {
		return "", fmt.Errorf("获取下载地址失败：%v", err)
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", errors.New("获取下载地址失败（无跳转目标）")
	}
	return loc, nil
}

// requestWithRetry 发起 GET 请求，遇到网关类错误时按 2 秒递增退避重试。
// 4xx（429 除外）视为确定性错误，直接返回，不做无意义的重试。
func requestWithRetry(ctx context.Context, client *http.Client, url string, headers map[string]string, accept func(*http.Response) bool) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt*2) * time.Second):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if accept(resp) {
			return resp, nil
		}

		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return nil, lastErr
		}
	}
	return nil, lastErr
}

func downloadSimple(ctx context.Context, url string, rep *progressReporter) ([]byte, error) {
	if url == "" {
		return nil, errors.New("没有可用的下载地址")
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt*2) * time.Second):
			}
		}
		data, err := downloadSimpleOnce(ctx, url, rep)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func downloadSimpleOnce(ctx context.Context, url string, rep *progressReporter) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := updateClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&progressWriter{w: &buf, p: rep}, resp.Body); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// downloadChunked 把文件切成若干段并发下载，失败的分片会重试。
func downloadChunked(ctx context.Context, url string, size int64, rep *progressReporter) ([]byte, error) {
	n := int64(chunks)
	if size < n*256<<10 {
		n = 4
	}
	chunkSize := (size + n - 1) / n
	out := make([]byte, size)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for i := int64(0); i < n; i++ {
		start := i * chunkSize
		if start >= size {
			break
		}
		end := start + chunkSize - 1
		if end >= size {
			end = size - 1
		}
		wg.Add(1)
		go func(start, end int64) {
			defer wg.Done()
			data, err := downloadRange(ctx, url, start, end, rep)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			copy(out[start:end+1], data)
		}(start, end)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	rep.finish()
	return out, nil
}

func downloadRange(ctx context.Context, url string, start, end int64, rep *progressReporter) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		data, err := downloadRangeOnce(ctx, url, start, end, rep)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func downloadRangeOnce(ctx context.Context, url string, start, end int64, rep *progressReporter) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := updateClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	want := end - start + 1
	var buf bytes.Buffer
	buf.Grow(int(want))
	if _, err := io.CopyBuffer(&progressWriter{w: &buf, p: rep}, resp.Body, make([]byte, 32<<10)); err != nil {
		return nil, err
	}
	if int64(buf.Len()) != want {
		return nil, errors.New("分片长度不符")
	}
	return buf.Bytes(), nil
}

// parseChecksum 从 checksums.txt 中取出指定文件的 SHA256。
func parseChecksum(data []byte, name string) (string, error) {
	lines := strings.Split(string(data), "\n")
	for _, ln := range lines {
		ln = strings.TrimPrefix(strings.TrimSpace(ln), "\ufeff")
		fields := strings.Fields(ln)
		if len(fields) >= 2 && strings.EqualFold(strings.TrimPrefix(fields[1], "*"), name) {
			if len(fields[0]) == 64 {
				return strings.ToLower(fields[0]), nil
			}
		}
	}
	for _, ln := range lines {
		ln = strings.TrimPrefix(strings.TrimSpace(ln), "\ufeff")
		fields := strings.Fields(ln)
		if len(fields) == 1 && len(fields[0]) == 64 {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("校验文件中没有 %s 的记录", name)
}

// versionLess 判断 a < b，按点分数字比较，忽略前导 v 与预发布后缀。
func versionLess(a, b string) bool {
	pa, pb := splitVersion(a), splitVersion(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func splitVersion(s string) []int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}
