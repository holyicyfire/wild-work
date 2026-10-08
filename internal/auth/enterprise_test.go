package auth

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEnterpriseFlagRoundTrip 企业标志在嵌套形 auth 文件中持久化；老文件（无 enterprise 字段）解析为 false。
func TestEnterpriseFlagRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// 企业账号：嵌套形带 enterprise=true
	entRaw := []byte(`{
	  "auth": {"accessToken":"at","refreshToken":"rt","expiresAt":1893456000,"domain":"trae.cn","apiHost":"https://trae.comnova.cc","machineId":"m","deviceId":"d"},
	  "account": {"uid":"u1","nickname":"ent","enterprise":true}
	}`)
	fp := filepath.Join(dir, "trae-u1.json")
	if err := os.WriteFile(fp, entRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := Parse(entRaw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !a.Enterprise {
		t.Fatal("enterprise should be true")
	}
	a.FilePath = fp
	if err := a.SaveAtomic(); err != nil {
		t.Fatalf("save: %v", err)
	}
	round, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := Parse(round)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if !a2.Enterprise || a2.ApiHost != "https://trae.comnova.cc" {
		t.Fatalf("roundtrip lost: enterprise=%v apiHost=%s", a2.Enterprise, a2.ApiHost)
	}

	// 老文件（个人账号，无 enterprise 字段）：向后兼容
	oldRaw := []byte(`{"auth":{"accessToken":"at","refreshToken":"rt","expiresAt":1,"domain":"trae.cn","apiHost":"https://api.trae.com.cn"},"account":{"uid":"u2"}}`)
	a3, err := Parse(oldRaw)
	if err != nil {
		t.Fatalf("parse old: %v", err)
	}
	if a3.Enterprise {
		t.Fatal("legacy file should default enterprise=false")
	}
}

// TestLoadTraeDirEnterpriseMixed 同一目录混合个人/企业账号，均按 traework 加载。
func TestLoadTraeDirEnterpriseMixed(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"trae-personal.json": `{"auth":{"accessToken":"at1","refreshToken":"rt","expiresAt":1},"account":{"uid":"p1"}}`,
		"trae-ent.json":      `{"auth":{"accessToken":"at2","refreshToken":"rt","expiresAt":1,"apiHost":"https://trae.comnova.cc"},"account":{"uid":"e1","enterprise":true}}`,
	}
	for name, raw := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	auths, err := LoadTraeDir(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(auths) != 2 {
		t.Fatalf("loaded=%d, want 2", len(auths))
	}
	entCount := 0
	for _, a := range auths {
		if a.Enterprise {
			entCount++
			if a.Kind != "traework" {
				t.Fatalf("enterprise kind=%s", a.Kind)
			}
		}
	}
	if entCount != 1 {
		t.Fatalf("enterprise accounts=%d, want 1", entCount)
	}
}
