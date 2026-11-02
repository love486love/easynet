//go:build windows

package creds

import (
	"os"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Secret{Username: "20240001", Password: "p@ss word", Carrier: "cmcc"}

	if err := Save(dir, want); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got != want {
		t.Fatalf("往返不一致: got %+v, want %+v", got, want)
	}
}

func TestSaveProducesCiphertext(t *testing.T) {
	dir := t.TempDir()
	secret := Secret{Username: "20240001", Password: "plaintext-marker", Carrier: "cmcc"}
	if err := Save(dir, secret); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	raw, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	for _, needle := range []string{"plaintext-marker", "20240001"} {
		if contains(raw, needle) {
			t.Fatalf("密文中出现了明文 %q", needle)
		}
	}
}

func contains(data []byte, needle string) bool {
	b := []byte(needle)
	for i := 0; i+len(b) <= len(data); i++ {
		match := true
		for j := range b {
			if data[i+j] != b[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
