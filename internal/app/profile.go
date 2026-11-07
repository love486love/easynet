// Package app 承载 CLI 与 GUI 入口共用的业务流程，避免多个 main 包重复实现。
package app

import (
	"path/filepath"

	"github.com/love486love/epo-2026/internal/config"
)

// LoadProfile 读取 profile：path 为空时使用 %LOCALAPPDATA%\epo\config.json。
// 配置缺失时回退到内置默认值，保证首次运行也能探针。
func LoadProfile(path string) (config.Profile, error) {
	if path != "" {
		return config.Load(path)
	}
	dir, err := config.UserDir()
	if err != nil {
		return config.Default(), nil
	}
	return config.Load(filepath.Join(dir, "config.json"))
}
