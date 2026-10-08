// entchat.go 企业版 Trae（IDE/CLI 产品线）对话协议：/api/ide/v2/llm_raw_chat。
//
// 与个人版 SOLO（llm_utils_chat + function=solo_work_lite）不同，企业租户按
// IDE/CLI 产品线鉴权（2026-10-08 实测：solo function 被业务码 4236 拒绝）。
//
// 协议依据：2026-07 mitmproxy 抓包（cpa-plugin/docs/trae-integration.md §2.4-2.6）
// + 2026-10-08 登录链路实测。响应 SSE 与 SOLO 同构（event: output/token_usage/done），
// 复用现有 solosse.go 解析器，未知事件（progress_notice）自动忽略。
package traework

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"wild-work/internal/auth"
	"wild-work/internal/provider"
)

const (
	// EntAppID trae-cli 客户端固定 X-App-Id（抓包 §2.4；与个人版 AppID 常量不同）
	EntAppID = "7b3f9dc2-8a4e-5c6d-2f1b-9e4a3c5b7df0"
	// EntEpRawChat 企业对话端点（IDE/CLI 产品线）
	EntEpRawChat = "/api/ide/v2/llm_raw_chat"
	// EntEpConfigList 企业模型配置端点（路由键 config_name/model_name 的权威来源）
	EntEpConfigList = "/api/ide/v1/cli/get_config_list"
	// 抓包中的客户端版本头
	EntIdeVersion     = "99.99.99"
	EntIdeVersionCode = "20260206"
)

// entModelEntry get_config_list 里单个模型的对话路由键。
type entModelEntry struct {
	ConfigName  string
	ModelName   string
	DisplayName string
}

// entConfigCache 模型配置缓存（10 分钟 TTL；key = 企业实例 host）。
type entConfigCache struct {
	at    time.Time
	items []entModelEntry
}

var entConfigCacheMap sync.Map // host -> *entConfigCache

func entHost(a *auth.Auth) string {
	if a.ApiHost != "" {
		return a.ApiHost
	}
	return EntDefaultConsoleHost
}

// chatStreamEnterprise 企业账号对话：llm_raw_chat。
func (c *Client) chatStreamEnterprise(a *auth.Auth, body []byte) (rc io.ReadCloser, status int, respBody []byte, err error) {
	model := entExtractModel(body)
	cfg, mn, display, err := c.entResolveModel(a, model)
	if err != nil {
		return nil, 0, nil, err
	}
	reqBody, sessID, err := entBuildChatBody(body, cfg, mn)
	if err != nil {
		return nil, 0, nil, err
	}
	host := entHost(a)
	req, err := http.NewRequest(http.MethodPost, host+EntEpRawChat, bytes.NewReader(reqBody))
	if err != nil {
		return nil, 0, nil, err
	}
	entChatHeaders(req, a, host, cfg, mn, display, sessID)
	hc := c.HTTP
	if c.StreamHTTP != nil {
		hc = c.StreamHTTP
	}
	resp, err := hc.Do(req)
	if err != nil {
		log.Printf("traework ent chat_stream uid=%s: transport error: %v", a.UID, err)
		return nil, 0, nil, err
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		kind := Classify(resp.StatusCode, string(raw))
		log.Printf("traework ent chat_stream uid=%s: upstream %d %s body=%s", a.UID, resp.StatusCode, kind, truncate(string(raw), 200))
		return nil, resp.StatusCode, raw, nil
	}
	return resp.Body, resp.StatusCode, nil, nil
}

// entExtractModel 从 OpenAI 请求体取 model 字段。
func entExtractModel(body []byte) string {
	var obj map[string]any
	if json.Unmarshal(body, &obj) != nil {
		return ""
	}
	s, _ := obj["model"].(string)
	return s
}

// entBuildChatBody 把 OpenAI chat/completions 请求体改写为 llm_raw_chat 格式。
// 依据抓包 §2.6 采用【白名单构造】：只发送上游已验证的键，
// 不透传 OpenAI 特有字段（stream_options/max_tokens/temperature/reasoning_effort 等
// 均导致上游 500，2026-10-08 实测）。
// 消息/tool 差异规则移植自 cpa-plugin（实测验证）：
//   - tools 的 function.parameters 必须是字符串化 JSON（对象直接上游 500）
//   - assistant 空 content 必须单空格占位（有 tool_calls 时也如此）
//   - assistant 的 tool_calls 改用 function_call 键 + index（非 OpenAI 的 function 键）
//   - tool 角色消息保留 tool_call_id，content 同样块化+空占位
func entBuildChatBody(src []byte, configName, modelName string) ([]byte, string, error) {
	var in map[string]any
	if len(src) > 0 && json.Unmarshal(src, &in) != nil {
		return nil, "", fmt.Errorf("ent chat body parse failed")
	}
	out := map[string]any{}
	if msgs, ok := in["messages"].([]any); ok {
		outMsgs := make([]any, 0, len(msgs))
		for _, mi := range msgs {
			m, ok := mi.(map[string]any)
			if !ok {
				continue
			}
			role, _ := m["role"].(string)
			if role == "developer" {
				role = "system"
			}
			switch {
			case role == "assistant" && m["tool_calls"] != nil:
				outMsgs = append(outMsgs, map[string]any{
					"role":       role,
					"content":    entAssistantBlocks(m["content"]),
					"tool_calls": entToolCallsJSON(m["tool_calls"]),
				})
			case role == "tool":
				outMsgs = append(outMsgs, map[string]any{
					"role":         "tool",
					"content":      entAssistantBlocks(m["content"]),
					"tool_call_id": m["tool_call_id"],
				})
			default:
				if s, ok := m["content"].(string); ok {
					m["content"] = []any{map[string]any{"type": "text", "text": s}}
				}
				m["role"] = role
				outMsgs = append(outMsgs, m)
			}
		}
		out["messages"] = outMsgs
	}
	if tools, ok := in["tools"].([]any); ok && len(tools) > 0 {
		out["tools"] = entToolsForTrae(tools)
	}
	sessID := genUUID()
	out["config_name"] = configName
	out["model_name"] = modelName
	out["conversation_id"] = genUUID()
	out["session_id"] = sessID
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, "", err
	}
	return raw, sessID, nil
}

// entToolsForTrae 把每个 tool 的 function.parameters 对象字符串化。
// Trae 严格要求 parameters 为 JSON 字符串；传对象上游返回 500（cpa-plugin 实测同款）。
func entToolsForTrae(tools []any) []any {
	for _, ti := range tools {
		t, ok := ti.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := t["function"].(map[string]any)
		if !ok {
			continue
		}
		p, ok := fn["parameters"]
		if !ok {
			continue
		}
		if _, isStr := p.(string); isStr {
			continue
		}
		if b, err := json.Marshal(p); err == nil {
			fn["parameters"] = string(b)
		}
	}
	return tools
}

// entAssistantBlocks assistant/tool 消息的内容块；空/纯空白内容 → 单空格占位
//（Trae 拒绝空 assistant content，即使携带 tool_calls；对齐真实 trae-cli 抓包）。
func entAssistantBlocks(content any) []any {
	if s, ok := content.(string); ok && strings.TrimSpace(s) != "" {
		return []any{map[string]any{"type": "text", "text": s}}
	}
	if arr, ok := content.([]any); ok && len(arr) > 0 {
		for _, bi := range arr {
			if b, ok := bi.(map[string]any); ok {
				if t, _ := b["text"].(string); strings.TrimSpace(t) != "" {
					return arr
				}
			}
		}
	}
	return []any{map[string]any{"type": "text", "text": " "}}
}

// entToolCallsJSON OpenAI tool_calls → Trae 线上格式：
// [{index, id, type:"function", function_call:{name, arguments}}]。
// 注意 Trae 用 function_call 键（非 OpenAI 的 function）。
func entToolCallsJSON(calls any) any {
	arr, ok := calls.([]any)
	if !ok {
		return calls
	}
	out := make([]map[string]any, 0, len(arr))
	for i, ci := range arr {
		c, ok := ci.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := c["function"].(map[string]any)
		name, _ := fn["name"].(string)
		args, _ := fn["arguments"].(string)
		id, _ := c["id"].(string)
		out = append(out, map[string]any{
			"index":         i,
			"id":            id,
			"type":          "function",
			"function_call": map[string]any{"name": name, "arguments": args},
		})
	}
	return out
}

// entChatHeaders 按抓包 §2.4 注入企业对话头。
func entChatHeaders(req *http.Request, a *auth.Auth, host, configName, modelName, display, sessionID string) {
	jwt := a.JWT()
	host = strings.TrimRight(host, "/")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Go-http-client/1.1")
	req.Header.Set("Authorization", "Cloud-IDE-JWT "+jwt)
	req.Header.Set("X-App-Id", EntAppID)
	req.Header.Set("X-Ide-Version", EntIdeVersion)
	req.Header.Set("X-Ide-Version-Code", EntIdeVersionCode)
	req.Header.Set("X-Ide-Function", "chat")
	trace := traceHex(16)
	req.Header.Set("X-Client-Request-Id", trace)
	req.Header.Set("X-Tt-Logid", trace)
	req.Header.Set("X-Flow-Traceparent", "00-"+traceHex(32)+"-"+traceHex(16)+"-01")
	if display == "" {
		display = configName
	}
	extra, _ := json.Marshal(map[string]any{
		"agent_loop_id":         genUUID(),
		"api_host":              host,
		"api_key":               jwt,
		"base_url":              host + "/trae-cli/api/v1/llm/proxy",
		"config_name":           configName,
		"config_source":         1,
		"display_name":          display,
		"model_name":            modelName,
		"real_api_key":          "",
		"real_base_url":         "",
		"session_id":            sessionID,
		"user_prompt_submit_id": genUUID(),
	})
	req.Header.Set("Extra", string(extra))
}

// entResolveModel 把用户请求的模型名解析为对话路由键 (config_name, model_name, display_name)。
// 权威来源 get_config_list；无匹配时原样透传（部分模型可能无 __dev/__v2 后缀）。
func (c *Client) entResolveModel(a *auth.Auth, model string) (cfg, mn, display string, err error) {
	if model == "" {
		model = DefaultConfigName
	}
	items, err := c.entConfigList(a)
	if err == nil {
		for _, it := range items {
			if strings.EqualFold(it.ConfigName, model) {
				mn = it.ModelName
				if mn == "" {
					mn = it.ConfigName
				}
				return it.ConfigName, mn, it.DisplayName, nil
			}
		}
	} else {
		log.Printf("traework ent config_list failed uid=%s err=%v（退化为原样模型名）", a.UID, err)
	}
	return model, model, model, nil
}

// entConfigList 调 get_config_list（带 10 分钟缓存）。抓包 §2.5：body {"function":"chat"}，
// 需要 X-App-Id（缺失报 4001 invalid app_id，2026-10-08 实测）。
func (c *Client) entConfigList(a *auth.Auth) ([]entModelEntry, error) {
	host := entHost(a)
	if v, ok := entConfigCacheMap.Load(host); ok {
		if cc, ok := v.(*entConfigCache); ok && time.Since(cc.at) < 10*time.Minute {
			return cc.items, nil
		}
	}
	raw, _ := json.Marshal(map[string]any{"function": "chat"})
	req, err := http.NewRequest(http.MethodPost, host+EntEpConfigList, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Go-http-client/1.1")
	req.Header.Set("Authorization", "Cloud-IDE-JWT "+a.JWT())
	req.Header.Set("X-App-Id", EntAppID)
	req.Header.Set("X-Ide-Version", EntIdeVersion)
	req.Header.Set("X-Ide-Version-Code", EntIdeVersionCode)
	req.Header.Set("X-Ide-Function", "chat")
	data, err := c.doJSON(req)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Code           int    `json:"code"`
		Message        string `json:"message"`
		ConfigInfoList []struct {
			ConfigName string `json:"config_name"`
			DisplayCfg struct {
				DisplayName string `json:"display_name"`
			} `json:"display_config"`
			ModelDetailList []struct {
				ModelName string `json:"model_name"`
			} `json:"model_detail_list"`
		} `json:"config_info_list"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("ent config_list parse: %w", err)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("ent config_list code=%d msg=%s", resp.Code, resp.Message)
	}
	items := make([]entModelEntry, 0, len(resp.ConfigInfoList))
	for _, ci := range resp.ConfigInfoList {
		if strings.TrimSpace(ci.ConfigName) == "" {
			continue
		}
		mn := ""
		if len(ci.ModelDetailList) > 0 {
			mn = ci.ModelDetailList[0].ModelName
		}
		items = append(items, entModelEntry{ConfigName: ci.ConfigName, ModelName: mn, DisplayName: ci.DisplayCfg.DisplayName})
	}
	entConfigCacheMap.Store(host, &entConfigCache{at: time.Now(), items: items})
	return items, nil
}

// FetchEntModels 企业账号模型列表（get_config_list；config_name 即 /v1/models 的 ID）。
func (c *Client) FetchEntModels(a *auth.Auth) ([]provider.ModelInfo, error) {
	items, err := c.entConfigList(a)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("ent config_list returned empty")
	}
	out := make([]provider.ModelInfo, 0, len(items))
	for _, it := range items {
		name := it.DisplayName
		if name == "" {
			name = it.ConfigName
		}
		out = append(out, provider.ModelInfo{ID: it.ConfigName, Name: name})
	}
	return out, nil
}

// genUUID v4 格式（会话/追踪 id 用）。
func genUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// traceHex n 字节随机 hex（trace id / traceparent 用）。
func traceHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
