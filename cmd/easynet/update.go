package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/minio/selfupdate"
)

const (
	releaseAPI = "https://api.github.com/repos/love486love/easynet/releases/latest"
	userAgent  = "easynet"
	// chunks 是分块下载的并发数。部分校园网出口对单连接限速在十几 KB/s，
	// 多连接可以叠加带宽；并发再调高反而会被出口限制，8 路实测最稳。
	chunks = 8
)

// assetRef 记录一个 Release 资产的两个下载入口：
// API 入口会 302 到真实文件域，能绕开被网关拦截的 github.com 直链。
type assetRef struct {
	API     string
	Browser string
	Size    int64
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPI, nil)
	if err != nil {
		return UpdateInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("无法连接 GitHub，请检查网络后重试")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return UpdateInfo{}, fmt.Errorf("查询更新失败（HTTP %d）", resp.StatusCode)
	}

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
		return UpdateInfo{}, fmt.Errorf("解析更新信息失败: %w", err)
	}

	info := UpdateInfo{
		Current: Version,
		Latest:  strings.TrimPrefix(rel.TagName, "v"),
		URL:     rel.HTMLURL,
		Body:    rel.Body,
	}
	for _, as := range rel.Assets {
		ref := assetRef{API: as.URL, Browser: as.Browser, Size: as.Size}
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
	return info, nil
}

// ApplyUpdate 下载并安装最新版本。绿色版原地替换 exe，安装版运行新版安装包。
// 成功后当前进程会退出，新版本自动启动。
func (a *App) ApplyUpdate() (string, error) {
	info, err := a.CheckUpdate()
	if err != nil {
		return "", err
	}
	if !info.HasUpdate {
		return "已是最新版本", nil
	}
	if !info.CanAuto {
		return "", fmt.Errorf("当前发布缺少自动更新所需的文件，请到 Release 页面手动下载")
	}

	if installedMode() {
		return applyInstaller(info)
	}
	return applyGreen(info)
}

// applyGreen 适用于绿色版：下载裸 exe、校验、原地替换。
func applyGreen(info UpdateInfo) (string, error) {
	if info.exe.API == "" || info.sum.API == "" {
		return "", fmt.Errorf("当前发布缺少 exe 或校验文件，请手动下载更新")
	}
	stopDaemonForUpdate()

	sumData, err := downloadAsset(info.sum)
	if err != nil {
		return "", fmt.Errorf("下载校验文件失败：%v", err)
	}
	want, err := parseChecksum(sumData, "easynet.exe")
	if err != nil {
		return "", err
	}

	data, err := downloadAsset(info.exe)
	if err != nil {
		return "", fmt.Errorf("下载新版本失败：%v", err)
	}

	got := sha256.Sum256(data)
	if !strings.EqualFold(fmt.Sprintf("%x", got), want) {
		return "", fmt.Errorf("文件校验失败，已取消更新，当前版本未改动")
	}

	if err := selfupdate.Apply(bytes.NewReader(data), selfupdate.Options{Checksum: got[:]}); err != nil {
		if rerr := selfupdate.RollbackError(err); rerr != nil {
			return "", fmt.Errorf("更新失败且自动回滚失败，请手动重新下载：%v", rerr)
		}
		return "", fmt.Errorf("更新失败，已保留旧版本：%v", err)
	}

	launchAfterExit()
	return "更新完成，正在重启…", nil
}

// downloadAsset 下载一个资产。先通过资产 API 换取带签名的真实地址，
// 再按文件大小选择单连接或并发分块。
func downloadAsset(ref assetRef) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	realURL, err := resolveAsset(ctx, ref.API)
	if err != nil {
		// API 不可用时退回直链；境外网络下直链通常更快。
		if ref.Browser == "" {
			return nil, err
		}
		realURL = ref.Browser
		ref.Size = 0
	}

	if ref.Size >= 1<<20 {
		return downloadChunked(ctx, realURL, ref.Size)
	}
	return downloadSimple(ctx, realURL)
}

// resolveAsset 用资产 API 换取 302 的真实下载地址。
func resolveAsset(ctx context.Context, apiURL string) (string, error) {
	if apiURL == "" {
		return "", errors.New("没有可用的下载地址")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusSeeOther &&
		resp.StatusCode != http.StatusTemporaryRedirect {
		return "", fmt.Errorf("获取下载地址失败（HTTP %d）", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", errors.New("获取下载地址失败（无跳转目标）")
	}
	return loc, nil
}

func downloadSimple(ctx context.Context, url string) ([]byte, error) {
	if url == "" {
		return nil, errors.New("没有可用的下载地址")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// downloadChunked 把文件切成若干段并发下载，失败的分片会重试。
func downloadChunked(ctx context.Context, url string, size int64) ([]byte, error) {
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
			data, err := downloadRange(ctx, url, start, end)
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
	return out, nil
}

func downloadRange(ctx context.Context, url string, start, end int64) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		data, err := downloadRangeOnce(ctx, url, start, end)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func downloadRangeOnce(ctx context.Context, url string, start, end int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	buf := make([]byte, end-start+1)
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		return nil, err
	}
	return buf, nil
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
