package traework

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wild-work/internal/auth"
	"wild-work/internal/provider"
)

// TestAgentBaseForRouting 企业账号 chat/models 路由到 ApiHost（企业单域名实例），
// 个人账号仍走全局 AgentHost（个人版 ApiHost 是 OAuth host，不能用作 chat host）。
func TestAgentBaseForRouting(t *testing.T) {
	c := New()
	if got := c.agentBaseFor(&auth.Auth{}); got != AgentHost {
		t.Fatalf("personal: got %s want %s", got, AgentHost)
	}
	if got := c.agentBaseFor(&auth.Auth{ApiHost: "https://api.trae.com.cn"}); got != AgentHost {
		t.Fatalf("personal with ApiHost: got %s want %s", got, AgentHost)
	}
	ent := &auth.Auth{Enterprise: true, ApiHost: "https://trae.comnova.cc"}
	if got := c.agentBaseFor(ent); got != "https://trae.comnova.cc" {
		t.Fatalf("enterprise: got %s want https://trae.comnova.cc", got)
	}
	if got := c.agentBaseFor(&auth.Auth{Enterprise: true}); got != AgentHost {
		t.Fatalf("enterprise empty ApiHost fallback: got %s want %s", got, AgentHost)
	}
}

// TestChatStreamEnterpriseRoutesToApiHost 验证 ChatStream 实际请求打到企业实例 host。
func TestChatStreamEnterpriseRoutesToApiHost(t *testing.T) {
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {}\n\n")
	}))
	defer srv.Close()

	// 企业账号 ApiHost 指向测试服务器（模拟企业单域名实例）
	u := srv.URL
	ent := &auth.Auth{Enterprise: true, ApiHost: u, AccessToken: "at"}
	c := New()
	c.HTTP = srv.Client()
	rc, status, _, err := c.ChatStream(ent, []byte(`{}`))
	if err != nil {
		t.Fatalf("chat stream: %v", err)
	}
	defer rc.Close()
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if gotHost != strings.TrimPrefix(u, "http://") {
		t.Fatalf("chat host=%s want %s", gotHost, u)
	}
}

// TestRefreshTokenEnterpriseDataFormat 企业版 ExchangeToken 响应读 Data.*（官方为 Result.*），
// 且请求 body 只含 RefreshToken（与官方前端行为对齐）。
func TestRefreshTokenEnterpriseDataFormat(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != EpExchange {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"code":0,"Data":{"Token":"tok2","RefreshToken":"rt2","TokenExpireAt":1893456000}}`))
	}))
	defer srv.Close()

	a := &auth.Auth{Enterprise: true, ApiHost: srv.URL, RefreshToken: "rt1", UID: "u1"}
	c := New()
	c.HTTP = srv.Client()
	if err := c.RefreshToken(a); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(gotBody) != 1 || gotBody["RefreshToken"] != "rt1" {
		t.Fatalf("enterprise body should only carry RefreshToken, got %v", gotBody)
	}
	if a.AccessToken != "tok2" || a.RefreshToken != "rt2" || a.ExpiresAt != 1893456000 {
		t.Fatalf("token=%q rt=%q exp=%d", a.AccessToken, a.RefreshToken, a.ExpiresAt)
	}
}

// TestRefreshTokenPersonalResultFormat 个人版仍用 Result.* 格式 + 完整 body（不变量：老路径零改动）。
func TestRefreshTokenPersonalResultFormat(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"Result":{"Token":"tok2","RefreshToken":"rt2","TokenExpireAt":1893456000}}`))
	}))
	defer srv.Close()

	a := &auth.Auth{ApiHost: srv.URL, RefreshToken: "rt1", UID: "u1"}
	c := New()
	c.HTTP = srv.Client()
	if err := c.RefreshToken(a); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if gotBody["ClientID"] != ClientID || gotBody["ClientSecret"] != "-" {
		t.Fatalf("personal body should carry ClientID/ClientSecret, got %v", gotBody)
	}
	if a.AccessToken != "tok2" || a.RefreshToken != "rt2" {
		t.Fatalf("token=%q rt=%q", a.AccessToken, a.RefreshToken)
	}
}

// TestRefreshTokenEnterpriseBusinessCode 企业端 HTTP 200 + code=30021（RefreshTokenInvalid）
// 应归类为 sessionDead（触发调用方 re-login），而非普通错误。
func TestRefreshTokenEnterpriseBusinessCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":30021,"message":"网络异常"}`))
	}))
	defer srv.Close()

	a := &auth.Auth{Enterprise: true, ApiHost: srv.URL, RefreshToken: "rt1", UID: "u1"}
	c := New()
	c.HTTP = srv.Client()
	err := c.RefreshToken(a)
	if err == nil {
		t.Fatal("want error")
	}
	pe, ok := err.(*provider.Error)
	if !ok || pe.Kind != provider.ErrSessionDead {
		t.Fatalf("err=%v (%T), want provider.Error ErrSessionDead", err, err)
	}
}

// TestDailyCheckinEnterpriseNoOp 企业账号签到直接 no-op，不打任何个人版 UgHost 接口。
func TestDailyCheckinEnterpriseNoOp(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New()
	c.HTTP = srv.Client()
	c.UgHost = srv.URL
	err := c.DailyCheckin(&auth.Auth{Enterprise: true, AccessToken: "at", UID: "u1"})
	if err == nil || !strings.Contains(err.Error(), "企业账号无签到活动") {
		t.Fatalf("err=%v, want 企业账号无签到活动", err)
	}
	if calls != 0 {
		t.Fatalf("enterprise checkin should not hit upstream, calls=%d", calls)
	}
}

// TestUserEntUsageEnterpriseNoOp 企业账号积分查询 no-op（企业积分由租户管理）。
func TestUserEntUsageEnterpriseNoOp(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New()
	c.HTTP = srv.Client()
	c.UgHost = srv.URL
	remain, err := c.UserEntUsage(&auth.Auth{Enterprise: true, AccessToken: "at", UID: "u1"})
	if err == nil || remain != 0 {
		t.Fatalf("remain=%d err=%v, want (0, error)", remain, err)
	}
	if calls != 0 {
		t.Fatalf("enterprise usage should not hit upstream, calls=%d", calls)
	}
}

// TestGetUserInfoEnterpriseDataFormat 企业实例 GetUserInfo 用 Data.UserInfo.* 信封
//（2026-10-08 实测响应体，官方为 Result.*），字段名 UserID/Name/EnterpriseID。
func TestGetUserInfoEnterpriseDataFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != EpUserInfo {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Cloudide-Token") != "at" {
			t.Errorf("missing X-Cloudide-Token header")
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"","Data":{"UserInfo":{"UserID":"6612729073695278","Name":"user@example.com","Account":"user@example.com","Email":"user@example.com","UserStatus":1,"RoleID":3},"EnterpriseID":"ent-1"}}`))
	}))
	defer srv.Close()

	a := &auth.Auth{Enterprise: true, ApiHost: srv.URL, AccessToken: "at"}
	c := New()
	c.HTTP = srv.Client()
	uid, nick, ent, err := c.GetUserInfo(a)
	if err != nil {
		t.Fatalf("userinfo: %v", err)
	}
	if uid != "6612729073695278" || nick == "" || ent != "ent-1" {
		t.Fatalf("uid=%q nick=%q ent=%q", uid, nick, ent)
	}
}

// TestGetUserInfoPersonalResultFormat 个人版仍读 Result.*（不变量：老路径零改动）。
func TestGetUserInfoPersonalResultFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Result":{"UserID":"p-1","ScreenName":"Leo","EnterpriseID":""}}`))
	}))
	defer srv.Close()

	a := &auth.Auth{ApiHost: srv.URL, AccessToken: "at"}
	c := New()
	c.HTTP = srv.Client()
	uid, nick, _, err := c.GetUserInfo(a)
	if err != nil || uid != "p-1" || nick != "Leo" {
		t.Fatalf("uid=%q nick=%q err=%v", uid, nick, err)
	}
}

// TestEntBuildChatBody OpenAI 体 → llm_raw_chat 体：content 块化、developer→system、
// 注入 config_name/model_name/conversation_id/session_id、剔除 stream/model/function。
func TestEntBuildChatBody(t *testing.T) {
	src := []byte(`{"model":"glm-5.2","stream":true,"messages":[
		{"role":"developer","content":"be brief"},
		{"role":"user","content":"hi"}]}`)
	out, sess, err := entBuildChatBody(src, "glm-5.2", "glm-5.2__dev")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if sess == "" {
		t.Fatal("session_id empty")
	}
	var m map[string]any
	if json.Unmarshal(out, &m) != nil {
		t.Fatal("output not json")
	}
	for _, k := range []string{"model", "stream", "function", "tool_choice"} {
		if _, ok := m[k]; ok {
			t.Fatalf("key %q should be removed", k)
		}
	}
	if m["config_name"] != "glm-5.2" || m["model_name"] != "glm-5.2__dev" {
		t.Fatalf("config/model = %v/%v", m["config_name"], m["model_name"])
	}
	msgs := m["messages"].([]any)
	first := msgs[0].(map[string]any)
	if first["role"] != "system" {
		t.Fatalf("developer not rewritten: %v", first["role"])
	}
	blocks := first["content"].([]any)
	blk := blocks[0].(map[string]any)
	if blk["type"] != "text" || blk["text"] != "be brief" {
		t.Fatalf("content block wrong: %v", blk)
	}
}

// TestEntConfigListResolve get_config_list 解析 + 模型路由键解析（抓包 §2.5 结构）。
func TestEntConfigListResolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != EntEpConfigList {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-App-Id") != EntAppID {
			t.Errorf("missing X-App-Id")
		}
		_, _ = w.Write([]byte(`{"code":0,"config_info_list":[
			{"config_name":"glm-5.2","display_config":{"display_name":"GLM-5.2"},
			 "model_detail_list":[{"model_name":"glm-5.2__dev"}]},
			{"config_name":"Doubao-Seed-2.1-pro","display_config":{"display_name":"Doubao-Seed-2.1-Pro"},
			 "model_detail_list":[{"model_name":"Doubao-Seed-2.1-pro__v2"}]}]}`))
	}))
	defer srv.Close()

	a := &auth.Auth{Enterprise: true, ApiHost: srv.URL, AccessToken: "at"}
	c := New()
	c.HTTP = srv.Client()
	cfg, mn, display, err := c.entResolveModel(a, "GLM-5.2") // 大小写不敏感
	if err != nil || cfg != "glm-5.2" || mn != "glm-5.2__dev" || display != "GLM-5.2" {
		t.Fatalf("cfg=%q mn=%q display=%q err=%v", cfg, mn, display, err)
	}
	models, err := c.FetchEntModels(a)
	if err != nil || len(models) != 2 || models[0].ID != "glm-5.2" {
		t.Fatalf("models=%v err=%v", models, err)
	}
}

// TestAggregateEnterpriseSSE 企业 llm_raw_chat SSE（含 progress_notice 非法 JSON 事件）
// 能被现有解析器正常聚合。
func TestAggregateEnterpriseSSE(t *testing.T) {
	sse := "event: progress_notice\ndata: ;Processing_abc\n\n" +
		"event: metadata\ndata: {\"model\":\"glm-5.2\",\"session_id\":\"s1\"}\n\n" +
		"event: output\ndata: {\"response\":\"成功\",\"reasoning_content\":null,\"tool_calls\":null,\"multimodal_contents\":null,\"phase\":null}\n\n" +
		"event: token_usage\ndata: {\"prompt_tokens\":5,\"completion_tokens\":1,\"total_tokens\":6}\n\n" +
		"event: done\ndata: {\"finish_reason\":\"stop\"}\n\n"
	res, err := Aggregate(strings.NewReader(sse))
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	msg := res["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "成功" {
		t.Fatalf("content=%v", msg["content"])
	}
}
