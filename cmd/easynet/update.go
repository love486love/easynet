package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

const releaseAPI = "https://api.github.com/repos/love486love/easynet/releases/latest"

// UpdateInfo 是一次更新检查的结果。
type UpdateInfo struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	HasUpdate   bool   `json:"has_update"`
	URL         string `json:"url"`
	Body        string `json:"body"`
	ExeURL      string `json:"exe_url"`
	SetupURL    string `json:"setup_url"`
	ChecksumURL string `json:"checksum_url"`
	CanAuto     bool   `json:"can_auto"`
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
	req.Header.Set("User-Agent", "easynet")

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
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
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
		switch {
		case as.Name == "easynet.exe":
			info.ExeURL = as.URL
		case as.Name == "checksums.txt":
			info.ChecksumURL = as.URL
		case strings.HasPrefix(as.Name, "easynet-setup-") && strings.HasSuffix(as.Name, ".exe"):
			info.SetupURL = as.URL
		}
	}
	info.HasUpdate = versionLess(info.Current, info.Latest)
	info.CanAuto = (info.ExeURL != "" && info.ChecksumURL != "") || info.SetupURL != ""
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
	if info.ExeURL == "" || info.ChecksumURL == "" {
		return "", fmt.Errorf("当前发布缺少 exe 或校验文件，请手动下载更新")
	}
	stopDaemonForUpdate()

	sumData, err := downloadAsset(info.ChecksumURL)
	if err != nil {
		return "", fmt.Errorf("下载校验文件失败：%v", err)
	}
	want, err := parseChecksum(sumData, "easynet.exe")
	if err != nil {
		return "", err
	}

	data, err := downloadAsset(info.ExeURL)
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

func downloadAsset(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "easynet")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
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
