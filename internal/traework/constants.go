// Package traework 封装 Trae SOLO 免费通道上游协议。
package traework

const (
	AgentHost      = "https://trae-api-cn.mchost.guru"
	UgHost         = "https://api.trae.cn"
	OAuthHost      = "https://api.trae.com.cn"
	ConsoleHost    = "https://www.trae.cn"
	WorkHost       = "https://work.trae.cn" // Web 应用端，模型定价接口用
	ClientID       = "en1oxy7wnw8j9n"
	AppID          = "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8"
	IdeVersion     = "0.1.52" // 对齐 connectedGraph/traework2api 上游，对应更新更全的模型配置表（含 glm-5.3）
	IdeVersionCode = "20260811"
	DeviceBrand    = "20Y5A002XX"   // 设备机型示例（指纹头用，可替换为真实机型，无需精确匹配）
	OSVersion      = "Windows 10 Pro" // 真实客户端系统版本
	PluginVersion  = "2.3.73734"     // 真实客户端插件版本（登录 URL 用）
	Function       = "solo_work_lite"

	EpChat          = "/api/agent/v3/llm_utils_chat"
	EpModels        = "/api/ide/v1/get_detail_param"
	EpExchange      = "/cloudide/api/v3/trae/oauth/ExchangeToken"
	EpUserInfo      = "/cloudide/api/v3/trae/GetUserInfo"
	EpCheckinStatus = "/trae/api/v2/ug/checkin_credits/status"
	EpCheckinClaim  = "/trae/api/v2/ug/checkin_credits/claim"
	EpEntUsage      = "/trae/api/v2/pay/web_user_ent_usage" // 网页版积分接口（带 require_usage 拿实际用量）
	EpModelsPricing = "/api/remote/v1/models"                    // 模型定价接口
)

// 企业版 Trae（ToB 单域名整栈部署，如 https://trae.comnova.cc）默认值。
// 与个人版共用同一套 Trae 服务端代码，核心 API 路径同构；
// 企业实例为单域名：console/oauth/agent 全部走同一 host，随账号持久化到 auth.apiHost。
// client_id 待实测：先试官方 solo id，不通则换企业 CLI id ono9krqynydwx5（配置可覆盖）。
const (
	EntDefaultConsoleHost = "https://trae.comnova.cc"
	EntDefaultClientID    = ClientID // 默认沿用官方 solo id；验证失败改用 ono9krqynydwx5
	EntAltClientID        = "ono9krqynydwx5"
	// 企业版默认 auth_from=trae（IDE 产品线）：实测企业套餐含 IDE/CLI 而不含 TRAE Work 时，
	// auth_from=solo 会在授权页被套餐校验拒绝；IDE 同样在 refreshToken 追加名单内，
	// 标准续期路径不受影响。CLI(traecli) 不在名单内，勿用。可通过 config 覆盖实测。
	EntDefaultAuthFrom = "trae"
)

// 企业版业务码（HTTP 200 + body code 非零；与企业版前端错误码枚举一致）
const (
	EntCodeRefreshInvalid = 30021 // RefreshTokenInvalid
	EntCodeNotLogin       = 30011 // 请先登录
)

const DefaultConfigName = "glm-5.2"
