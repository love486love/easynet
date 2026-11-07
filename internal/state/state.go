// Package state 在 %LOCALAPPDATA%\epo 下维护守护进程的运行快照。
// GUI 通过该文件判断守护是否在跑，并通过 stop.flag 请求优雅退出，
// 避免跨进程杀进程带来的权限与误杀问题。
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Snapshot 是守护进程写出的最后一帧状态。
type Snapshot struct {
	State     string    `json:"state"` // online / offline / unknown / stopped
	Message   string    `json:"message"`
	PID       int       `json:"pid"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Path 返回状态文件路径。
func Path(dir string) string { return filepath.Join(dir, "state.json") }

// StopPath 返回停止请求标记路径。
func StopPath(dir string) string { return filepath.Join(dir, "stop.flag") }

// Write 原子写入快照：先写临时文件再改名，避免 GUI 读到半个 JSON。
func Write(dir string, s Snapshot) error {
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = time.Now()
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := Path(dir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, Path(dir)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Read 读取快照；文件不存在时返回错误。
func Read(dir string) (Snapshot, error) {
	data, err := os.ReadFile(Path(dir))
	if err != nil {
		return Snapshot{}, err
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return Snapshot{}, fmt.Errorf("解析守护状态失败: %w", err)
	}
	return s, nil
}

// RequestStop 写入停止标记，守护在下一轮循环检查后退出。
func RequestStop(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(StopPath(dir), []byte(time.Now().Format(time.RFC3339)), 0o600)
}

// StopRequested 报告是否存在停止标记。
func StopRequested(dir string) bool {
	_, err := os.Stat(StopPath(dir))
	return err == nil
}

// ClearStop 删除停止标记；不存在时视为成功。
func ClearStop(dir string) error {
	err := os.Remove(StopPath(dir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
