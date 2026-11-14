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
	// 未标校区的账号按东区落盘与回读（规范化是设计行为）。
	want.Campus = CampusDongqu
	if got != want {
		t.Fatalf("往返不一致: got %+v, want %+v", got, want)
	}
}

// 分校区：东区/本部各有一份当前账号，互不串号。
func TestCampusAccounts(t *testing.T) {
	dir := t.TempDir()
	dq := Secret{Username: "6020240001", Password: "p1", Carrier: "cmcc", Campus: CampusDongqu}
	bb := Secret{Username: "9001", Password: "p2", Carrier: "campus", Campus: CampusBenbu}

	if _, err := SaveAccount(dir, dq); err != nil {
		t.Fatalf("保存东区失败: %v", err)
	}
	if _, err := SaveAccount(dir, bb); err != nil {
		t.Fatalf("保存本部失败: %v", err)
	}

	gotBB, err := LoadForCampus(dir, CampusBenbu)
	if err != nil || gotBB.Username != "9001" {
		t.Fatalf("本部账号错误: %+v, %v", gotBB, err)
	}
	gotDQ, err := LoadForCampus(dir, CampusDongqu)
	if err != nil || gotDQ.Username != "6020240001" {
		t.Fatalf("东区账号错误: %+v, %v", gotDQ, err)
	}

	// 东区列表里没有第二个账号时，本部取账号不会串到东区。
	if _, err := LoadForCampus(dir, "other"); err == nil {
		t.Fatal("未知校区应返回错误")
	}

	// 删除本部当前账号：本部回退到本部列表剩余账号；东区不受影响。
	if _, err := DeleteAccount(dir, "9001"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := LoadForCampus(dir, CampusBenbu); err == nil {
		t.Fatal("本部无账号时应返回明确错误")
	}
	gotDQ, err = LoadForCampus(dir, CampusDongqu)
	if err != nil || gotDQ.Username != "6020240001" {
		t.Fatalf("删除后东区账号不应受影响: %+v, %v", gotDQ, err)
	}
}

// 旧版 store（无 campus/currents 字段）读取时规范化为东区 + 由 legacy current 推导。
func TestLegacyStoreNormalizesToDongqu(t *testing.T) {
	dir := t.TempDir()
	enc, err := protect([]byte(`{"accounts":[{"username":"20240001","password":"p","carrier":"cmcc"}],"current":"20240001"}`))
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if err := os.WriteFile(StorePath(dir), enc, filePerm); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}

	store, err := LoadStore(dir)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if store.Accounts[0].Campus != CampusDongqu {
		t.Errorf("旧账号应规范化为东区: %q", store.Accounts[0].Campus)
	}
	if store.Currents[CampusDongqu] != "20240001" {
		t.Errorf("legacy current 应推导为东区当前: %v", store.Currents)
	}
	got, err := LoadForCampus(dir, CampusDongqu)
	if err != nil || got.Username != "20240001" {
		t.Fatalf("东区账号错误: %+v, %v", got, err)
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
