//go:build windows

package creds

import (
	"os"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Secret{Username: "20240001", Password: "p@ss word", Carrier: "cmcc"}

	if _, err := SaveAccount(dir, want); err != nil {
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
	if _, err := SaveAccount(dir, secret); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	raw, err := os.ReadFile(StorePath(dir))
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	for _, needle := range []string{"plaintext-marker", "20240001"} {
		if contains(raw, needle) {
			t.Fatalf("密文中出现了明文 %q", needle)
		}
	}
}

func TestMultipleAccounts(t *testing.T) {
	dir := t.TempDir()
	a := Secret{Username: "20240001", Password: "p1", Carrier: "cmcc"}
	b := Secret{Username: "20240002", Password: "p2", Carrier: "ctcc"}

	if _, err := SaveAccount(dir, a); err != nil {
		t.Fatalf("保存 a 失败: %v", err)
	}
	if _, err := SaveAccount(dir, b); err != nil {
		t.Fatalf("保存 b 失败: %v", err)
	}

	store, err := LoadStore(dir)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(store.Accounts) != 2 {
		t.Fatalf("应有 2 个账号，实际 %d", len(store.Accounts))
	}
	if store.Current != b.Username {
		t.Fatalf("当前账号应为最近保存的 b，实际 %s", store.Current)
	}

	if _, err := SelectAccount(dir, a.Username); err != nil {
		t.Fatalf("切换失败: %v", err)
	}
	cur, err := Load(dir)
	if err != nil || cur.Username != a.Username {
		t.Fatalf("切换后当前账号错误: %+v, %v", cur, err)
	}

	if _, err := DeleteAccount(dir, a.Username); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	store, _ = LoadStore(dir)
	if len(store.Accounts) != 1 || store.Current != b.Username {
		t.Fatalf("删除后应只剩 b 且为当前: %+v", store)
	}
}

func TestMigratesLegacyFile(t *testing.T) {
	dir := t.TempDir()
	old := Secret{Username: "20240001", Password: "legacy", Carrier: "cmcc"}

	raw, err := protect([]byte(`{"username":"20240001","password":"legacy","carrier":"cmcc"}`))
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if err := os.WriteFile(Path(dir), raw, filePerm); err != nil {
		t.Fatalf("写旧文件失败: %v", err)
	}

	store, err := LoadStore(dir)
	if err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	if len(store.Accounts) != 1 || store.Current != old.Username || store.Accounts[0].Password != old.Password {
		t.Fatalf("迁移结果错误: %+v", store)
	}
	if _, err := os.Stat(Path(dir)); !os.IsNotExist(err) {
		t.Fatalf("迁移后旧文件应已删除")
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
