package login_trae

import (
	"net/url"
	"testing"

	"wild-work/internal/traework"
)

// TestBuildAuthURLEnterprise 企业渠道：授权页指向企业实例 host，auth_from 仍为 solo
//（企业枚举含 solo，回调会追加 refreshToken），PKCE 参数保留（企业端忽略）。
func TestBuildAuthURLEnterprise(t *testing.T) {
	opts := LoginOpts{Enterprise: true}.withDefaults()
	if opts.ConsoleHost != traework.EntDefaultConsoleHost {
		t.Fatalf("console host=%s, want %s", opts.ConsoleHost, traework.EntDefaultConsoleHost)
	}
	if opts.ClientID != traework.ClientID {
		t.Fatalf("client id=%s, want %s", opts.ClientID, traework.ClientID)
	}
	callback := "http://127.0.0.1:57209/authorize"
	raw := buildAuthURL(opts, callback, "m1", "d1", "cc")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if u.Host != "trae.comnova.cc" || u.Path != "/authorization" {
		t.Fatalf("host/path=%s%s, want trae.comnova.cc/authorization", u.Host, u.Path)
	}
	q := u.Query()
	if q.Get("auth_from") != "solo" {
		t.Fatalf("auth_from=%s, want solo", q.Get("auth_from"))
	}
	if q.Get("client_id") != traework.ClientID {
		t.Fatalf("client_id=%s", q.Get("client_id"))
	}
	if q.Get("auth_callback_url") != callback {
		t.Fatalf("auth_callback_url=%s", q.Get("auth_callback_url"))
	}
	if q.Get("code_challenge") == "" {
		t.Fatal("code_challenge should be present (enterprise page ignores it)")
	}
}

// TestBuildAuthURLPersonal 个人渠道：官方入口 + 官方 client_id，行为零变化。
func TestBuildAuthURLPersonal(t *testing.T) {
	opts := LoginOpts{}.withDefaults()
	if opts.ConsoleHost != traework.ConsoleHost {
		t.Fatalf("console host=%s, want %s", opts.ConsoleHost, traework.ConsoleHost)
	}
	raw := buildAuthURL(opts, "http://127.0.0.1:1/authorize", "m", "d", "cc")
	u, _ := url.Parse(raw)
	if u.Host != "www.trae.cn" {
		t.Fatalf("host=%s, want www.trae.cn", u.Host)
	}
	q := u.Query()
	if q.Get("client_id") != traework.ClientID || q.Get("auth_from") != "solo" {
		t.Fatalf("client_id=%s auth_from=%s", q.Get("client_id"), q.Get("auth_from"))
	}
}

// TestLoginOptsOverride 配置覆盖：企业渠道可自定义 host 与 client_id。
func TestLoginOptsOverride(t *testing.T) {
	opts := LoginOpts{Enterprise: true, ConsoleHost: "https://ent.example.com", ClientID: "custom_id"}.withDefaults()
	if opts.ConsoleHost != "https://ent.example.com" || opts.ClientID != "custom_id" {
		t.Fatalf("override lost: %+v", opts)
	}
}

// TestParseCallbackEnterpriseRefreshToken 企业 solo 回调携带 refreshToken 参数，解析路径与个人版一致。
func TestParseCallbackEnterpriseRefreshToken(t *testing.T) {
	info := ParseCallback("https://trae.comnova.cc/authorize?host=https%3A%2F%2Ftrae.comnova.cc&refreshToken=rt123")
	if info.RefreshToken != "rt123" {
		t.Fatalf("refreshToken=%q", info.RefreshToken)
	}
}
