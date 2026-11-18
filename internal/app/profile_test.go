package app

import (
	"path/filepath"
	"testing"

	"github.com/love486love/easynet/internal/config"
)

// 界面把配置写在 profile.json，守护必须读到同一个文件；
// 早期守护读的是 config.json，两者不一致会让校区偏好永远不生效。
func TestLoadProfileUsesProfileJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)

	p := config.Default()
	p.Network.PreferCampus = config.CampusBenbu
	if err := config.Save(filepath.Join(dir, "easynet", "profile.json"), p); err != nil {
		t.Fatal(err)
	}

	got, err := LoadProfile("")
	if err != nil {
		t.Fatal(err)
	}
	if got.Network.PreferCampus != config.CampusBenbu {
		t.Fatalf("PreferCampus = %q，期望 %q", got.Network.PreferCampus, config.CampusBenbu)
	}
}

// profile.json 不存在时回退读取旧文件名，升级后配置不丢。
func TestLoadProfileFallsBackToConfigJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)

	p := config.Default()
	p.Network.PreferCampus = config.CampusBenbu
	if err := config.Save(filepath.Join(dir, "easynet", "config.json"), p); err != nil {
		t.Fatal(err)
	}

	got, err := LoadProfile("")
	if err != nil {
		t.Fatal(err)
	}
	if got.Network.PreferCampus != config.CampusBenbu {
		t.Fatalf("PreferCampus = %q，期望 %q", got.Network.PreferCampus, config.CampusBenbu)
	}
}

// 两个文件都不存在时使用内置默认值。
func TestLoadProfileDefaultsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)

	got, err := LoadProfile("")
	if err != nil {
		t.Fatal(err)
	}
	if got.Network.PreferCampus != config.CampusDongqu {
		t.Fatalf("PreferCampus = %q，期望默认 %q", got.Network.PreferCampus, config.CampusDongqu)
	}
}
