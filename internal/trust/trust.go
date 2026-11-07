// Package trust 保存"首次信任"的网关 MAC 列表（TOFU）。
// 与 profile 分离：信任是运行态数据，不属于可分享的配置。
package trust

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxEntries = 8
	fileName   = "trusted_macs.json"
)

// Path 返回信任列表文件路径。
func Path(dir string) string {
	return filepath.Join(dir, fileName)
}

// Load 读取信任列表；文件不存在时返回空列表。
func Load(dir string) ([]string, error) {
	data, err := os.ReadFile(Path(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return normalizeList(list), nil
}

// Remember 把 MAC 记为最新信任项：去重、移到最前、超过 8 条则淘汰最旧的。
func Remember(dir, mac string) ([]string, error) {
	list, err := Load(dir)
	if err != nil {
		return nil, err
	}

	item := normalize(mac)
	if item == "" {
		return list, nil
	}

	out := []string{item}
	for _, v := range list {
		if v != item {
			out = append(out, v)
		}
	}
	if len(out) > maxEntries {
		out = out[:maxEntries]
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return out, os.WriteFile(Path(dir), data, 0o644)
}

func normalize(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "-", ":"))
}

func normalizeList(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		n := normalize(v)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	if len(out) > maxEntries {
		out = out[:maxEntries]
	}
	return out
}
