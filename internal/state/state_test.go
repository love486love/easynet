package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := Snapshot{State: "online", Message: "", PID: 1234}
	if err := Write(dir, in); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	out, err := Read(dir)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if out.State != in.State || out.PID != in.PID {
		t.Fatalf("内容不一致: %+v", out)
	}
	if out.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt 未填充")
	}
	if time.Since(out.UpdatedAt) > time.Minute {
		t.Fatalf("UpdatedAt 明显偏差: %v", out.UpdatedAt)
	}
}

func TestReadMissing(t *testing.T) {
	if _, err := Read(t.TempDir()); err == nil {
		t.Fatal("缺失文件应当报错")
	}
}

func TestStopFlagLifecycle(t *testing.T) {
	dir := t.TempDir()
	if StopRequested(dir) {
		t.Fatal("初始不应有停止标记")
	}
	if err := RequestStop(dir); err != nil {
		t.Fatalf("写标记失败: %v", err)
	}
	if !StopRequested(dir) {
		t.Fatal("停止标记未生效")
	}
	if err := ClearStop(dir); err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if StopRequested(dir) {
		t.Fatal("停止标记未清理")
	}
	if err := ClearStop(dir); err != nil {
		t.Fatalf("重复清理应成功: %v", err)
	}
}

func TestWriteCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "easynet")
	if err := Write(dir, Snapshot{State: "unknown"}); err != nil {
		t.Fatalf("应自动创建目录: %v", err)
	}
	if _, err := os.Stat(Path(dir)); err != nil {
		t.Fatalf("状态文件不存在: %v", err)
	}
}
