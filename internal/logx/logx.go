// Package logx 提供结构化日志、脱敏与简单轮转。
// 任何可能包含凭据的字段都必须先过 MaskURL / MaskAccount。
package logx

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
)

const (
	maxLogSize = 5 << 20 // 5 MB
	keepFiles  = 3
)

var sensitiveParams = regexp.MustCompile(`(user_password|wlan_user_mac|usermac|user_account)=([^&"'\s]+)`)

// New 在 dir 下创建 epo.log 并返回 JSON 行格式的 logger。
// 返回的 close 函数必须在退出前调用。
func New(dir string) (*slog.Logger, func() error, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(dir, "epo.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogSize {
		rotate(path)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}

	handler := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(handler), f.Close, nil
}

func rotate(path string) {
	_ = os.Remove(fmt.Sprintf("%s.%d", path, keepFiles-1))
	for i := keepFiles - 2; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", path, i), fmt.Sprintf("%s.%d", path, i+1))
	}
	_ = os.Rename(path, path+".1")
}

// MaskURL 把 URL 中的密码、MAC 等敏感参数替换为 ***。
func MaskURL(s string) string {
	return sensitiveParams.ReplaceAllString(s, "$1=***")
}

// MaskAccount 保留账号首尾各两位，其余打码。
func MaskAccount(s string) string {
	r := []rune(s)
	if len(r) <= 4 {
		return "***"
	}
	return string(r[:2]) + "****" + string(r[len(r)-2:])
}
