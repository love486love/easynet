// Package app 承载 CLI 与 GUI 入口共用的业务流程，避免多个 main 包重复实现。
package app

import (
	"os"
	"path/filepath"

	"github.com/love486love/easynet/internal/config"
)

// LoadProfile 读取 profile。
//
// path 为空时使用 %LOCALAPPDATA%\easynet\profile.json —— 与界面保存的配置
// 是同一个文件。旧版本的守护读的是 config.json，若该文件存在而
// profile.json 不存在则回退读取，避免升级后配置丢失。
func LoadProfile(path string) (config.Profile, error) {
	if path != "" {
		return config.Load(path)
	}
	dir, err := config.UserDir()
	if err != nil {
		return config.Default(), nil
	}
	if _, err := os.Stat(filepath.Join(dir, "profile.json")); err == nil {
		return config.Load(filepath.Join(dir, "profile.json"))
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
		return config.Load(filepath.Join(dir, "config.json"))
	}
	return config.Default(), nil
}
