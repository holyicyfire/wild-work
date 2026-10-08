// models.go 管理面板「可用模型」：按渠道/账号类型聚合可调用模型列表。
//
// 企业版 Trae 与个人版 TraeWork 模型列表不同源（get_config_list vs get_detail_param），
// 按账号的企业标志分别拉取；动态获取失败时回落各渠道静态名单。
package app

import (
	"time"

	"wild-work/internal/auth"
	"wild-work/internal/provider"
	"wild-work/internal/qoder"
	"wild-work/internal/server"
)

const modelsCacheTTL = 10 * time.Minute

// ModelItem 单个模型（ID 即 /v1 调用用的 model id，含渠道前缀由前端拼接展示）。
type ModelItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GroupModels 单个渠道/账号类型的模型列表。
type GroupModels struct {
	Group  string      `json:"group"` // workbuddy | traework | traework_ent | qoder
	Label  string      `json:"label"`
	Models []ModelItem `json:"models"`
	Source string      `json:"source"` // dynamic（在线获取）| static（静态兜底）
	Count  int         `json:"count"`
	Error  string      `json:"error,omitempty"` // 动态获取失败原因（有兜底时非致命）
}

type modelsCacheEntry struct {
	at time.Time
	gm GroupModels
}

// pickAccount 取池中第一个启用且企业标志匹配的账号。
func (a *App) pickAccount(kind provider.Kind, enterprise bool) *auth.Auth {
	rt := a.runtimes[kind]
	if rt == nil {
		return nil
	}
	for _, st := range rt.Pool.List() {
		if st.Disabled || st.Enterprise != enterprise {
			continue
		}
		return rt.Pool.AuthByUID(st.UID)
	}
	return nil
}

// ModelsState 聚合各渠道/账号类型的模型列表（缓存 modelsCacheTTL；force 跳过缓存）。
func (a *App) ModelsState(force bool) map[string]any {
	groups := []struct {
		key   string
		label string
		kind  provider.Kind
		ent   bool
	}{
		{"workbuddy", "WorkBuddy", provider.WorkBuddy, false},
		{"traework", "TraeWork（个人）", provider.TraeWork, false},
		{"traework_ent", "Trae 企业版", provider.TraeWork, true},
		{"qoder", "Qoder", provider.Qoder, false},
	}
	a.muModels.Lock()
	defer a.muModels.Unlock()
	if a.modelsCache == nil {
		a.modelsCache = map[string]modelsCacheEntry{}
	}
	out := make([]GroupModels, 0, len(groups))
	for _, g := range groups {
		if ce, ok := a.modelsCache[g.key]; ok && !force && time.Since(ce.at) < modelsCacheTTL {
			out = append(out, ce.gm)
			continue
		}
		gm := a.buildGroupModels(g.kind, g.ent, g.label, g.key)
		a.modelsCache[g.key] = modelsCacheEntry{at: time.Now(), gm: gm}
		out = append(out, gm)
	}
	return map[string]any{"groups": out, "generated_at": time.Now().Format("2006-01-02 15:04:05")}
}

// buildGroupModels 单组：在线获取优先，失败回落静态名单。
func (a *App) buildGroupModels(kind provider.Kind, ent bool, label, key string) GroupModels {
	gm := GroupModels{Group: key, Label: label, Models: []ModelItem{}, Source: "dynamic"}
	acct := a.pickAccount(kind, ent)
	if acct == nil {
		gm.Source = "static"
		gm.Error = "暂无可用账号，登录后自动获取动态列表"
		gm.Models = staticModelFallback(kind)
		gm.Count = len(gm.Models)
		return gm
	}
	rt := a.runtimes[kind]
	infos, err := rt.Upstream.FetchModels(acct)
	if err != nil {
		gm.Source = "static"
		gm.Error = "在线获取失败：" + err.Error()
		gm.Models = staticModelFallback(kind)
	} else {
		for _, mi := range infos {
			gm.Models = append(gm.Models, ModelItem{ID: mi.ID, Name: mi.Name})
		}
	}
	gm.Count = len(gm.Models)
	return gm
}

// staticModelFallback 各渠道静态兜底名单。
func staticModelFallback(kind provider.Kind) []ModelItem {
	var infos []provider.ModelInfo
	switch kind {
	case provider.WorkBuddy:
		infos = server.WorkBuddyStaticModels()
	case provider.Qoder:
		infos = qoder.StaticModels()
	default:
		infos = server.TraeWorkStaticModels()
	}
	out := make([]ModelItem, 0, len(infos))
	for _, mi := range infos {
		out = append(out, ModelItem{ID: mi.ID, Name: mi.Name})
	}
	return out
}
