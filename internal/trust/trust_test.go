package trust

import (
	"testing"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	list, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("文件不存在不应报错: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("应为空列表，实际 %v", list)
	}
}

func TestRememberDedupAndMoveToFront(t *testing.T) {
	dir := t.TempDir()

	list, err := Remember(dir, "AA-BB-CC-DD-EE-FF")
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if len(list) != 1 || list[0] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("应归一化为冒号小写，实际 %v", list)
	}

	list, _ = Remember(dir, "11:22:33:44:55:66")
	if len(list) != 2 || list[0] != "11:22:33:44:55:66" {
		t.Fatalf("新条目应在最前，实际 %v", list)
	}

	list, _ = Remember(dir, "aa:bb:cc:dd:ee:ff")
	if len(list) != 2 || list[0] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("重复条目应去重并前置，实际 %v", list)
	}
}

func TestRememberCap(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 12; i++ {
		mac := string(rune('a'+i)) + "a:bb:cc:dd:ee:ff"
		if _, err := Remember(dir, mac); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}
	list, err := Load(dir)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(list) != maxEntries {
		t.Fatalf("上限应为 %d，实际 %d", maxEntries, len(list))
	}
}
