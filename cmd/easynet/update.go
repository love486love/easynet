package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const releaseAPI = "https://api.github.com/repos/love486love/easynet/releases/latest"

// UpdateInfo 是一次更新检查的结果。
type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	HasUpdate bool   `json:"has_update"`
	URL       string `json:"url"`
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
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return UpdateInfo{}, fmt.Errorf("解析更新信息失败: %w", err)
	}

	info := UpdateInfo{
		Current: Version,
		Latest:  strings.TrimPrefix(rel.TagName, "v"),
		URL:     rel.HTMLURL,
	}
	info.HasUpdate = versionLess(info.Current, info.Latest)
	return info, nil
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
