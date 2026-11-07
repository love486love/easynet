package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadCredentialToleratesBOM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cred.json")

	body := `{"username":"20240001","password":"p@ss","carrier":"cmcc"}`
	content := append([]byte{0xEF, 0xBB, 0xBF}, []byte(body)...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	cred, err := readCredential(path)
	if err != nil {
		t.Fatalf("带 BOM 的 JSON 应能解析: %v", err)
	}
	if cred.Username != "20240001" || cred.Carrier != "cmcc" {
		t.Fatalf("解析结果不正确: %+v", cred)
	}
}

func TestReadCredentialRejectsMissingFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cred.json")

	if err := os.WriteFile(path, []byte(`{"username":"u"}`), 0o600); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if _, err := readCredential(path); err == nil {
		t.Fatal("缺少 password 时应报错")
	}
}
