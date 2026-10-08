# 企业版 Trae 接入 — Phase 2 真实账号验证清单

> Phase 1 代码改造已完成（`feat/enterprise-trae` 分支）。本清单需在**公司网络内 + 真实企业账号**下按序执行，
> 对应调研纪要 §6 的待验证项。每项给出预期结果与不一致时的处理分支。

## 0. 准备

```bash
go build -o dist/wild-work.exe ./cmd/wild-work && ./dist/wild-work.exe
# 打开面板 http://127.0.0.1:7863，点「＋ Trae 企业版」
```

## 1. client_id / auth_from 校验（纪要 §6.1，2026-10-08 实测更新）

**实测发现**：`auth_from=solo`（TRAE Work 产品线）在企业授权页被套餐校验拒绝：
「当前企业账号暂不支持 TRAE Work 使用，请联系管理员升级套餐或使用 TRAE IDE」。企业套餐包含 IDE 与 CLI。

**对策（已实施）**：企业渠道默认 `auth_from=trae`（IDE 产品线）——IDE 同样在 refreshToken 追加名单内，
标准续期链路不受影响；CLI(traecli) 不在名单内，不可用。可配置覆盖。

**测试矩阵**（按序试，改 `config.json` 后重启）：

| # | auth_from | client_id | 说明 |
|---|-----------|-----------|------|
| 1 | `trae`（默认） | 空（官方 solo id） | 首选组合 |
| 2 | `trae` | `ono9krqynydwx5`（企业 CLI id） | 企业端校验 client_id 与 auth_from 配套时 |
| 3 | `vscode` / `jetbrains` | 同上递试 | 均在 refreshToken 追加名单内，属套餐内客户端的备选 |

- **预期 A**：授权页通过，回调带 refreshToken → 登录完成，进入 §2。
- **预期 B**：仍报套餐不支持 → 说明该租户连 IDE 也不授权此流程（或需管理员开通），到此为止找管理员。

## 2. 登录回调与续期（纪要 §6.2）

**步骤**：浏览器完成登录后观察日志：

- `traework 登录成功 uid=...` → 回调带回了 refreshToken（solo 名单生效 ✓）
- `traework refresh start/success` → ExchangeToken 正常

**关注点**：

| 现象 | 结论 | 处理 |
|------|------|------|
| 登录成功且次日自动续期成功 | `Result.*` 或 `Data.*` 双格式兼容已覆盖 ✓ | 无 |
| 登录成功但 refresh 报 `exchange parse` / `no token` | 响应字段名在 `Result/Data` 之外 | 抓响应体（日志有前 200 字符），在 `RefreshToken` 再加一个候选字段 |
| refresh 报 `enterprise code=30021` | refreshToken 无效/过期 | 重新登录；若反复出现说明企业 refreshToken 轮换策略不同，需调整保存逻辑 |

## 3. chat 兼容性（纪要 §6.3，含新风险项）

**前提**：IDE 认证拿到 token 后，下一个待验证项就是 IDE 产品线是否覆盖 solo chat 接口：
`llm_utils_chat` 是否接受 `function=solo_work_lite`。若服务端以业务码拒绝（如产品线不匹配），
说明 IDE 授权不含该 API——那就是产品线确实未覆盖，找管理员开通 TRAE Work 才是正解，不要强行伪装。

**步骤**：用任一 OpenAI 客户端发 `traework/<model>` 请求（模型列表先看第 4 步）。

- **预期**：流式响应正常。
- **若 4xx/业务错误**：企业端可能不认 `function=solo_work_lite` payload。
  排查：`curl -sSk -X POST -H "Authorization: Cloud-IDE-JWT <token>" -H "Content-Type: application/json" -d '{"function":"solo_work_lite",...}' "https://trae.comnova.cc/api/agent/v3/llm_utils_chat"`
  对照企业 CLI 抓包的 function 名，改 `constants.go` 的 `Function` 常量（或按 Enterprise 分流）。

## 4. 模型列表（纪要 §6.4）

**步骤**：面板模型下拉或 `GET /v1/models`，确认返回企业实例的模型集合。

- **预期**：`get_detail_param` 对企业 token 有效，返回企业可用模型。
- **若为空/401**：企业版模型接口路径可能不同（企业前端 JS chunk 里有线索），按纪要 §7 方法重新逆向。

## 5. 面板行为核对

- [ ] 企业账号卡片显示「Trae 企业版」徽标，签到按钮置灰
- [ ] 手动点签到 → 返回「企业账号无需签到」
- [ ] 刷新积分 → 提示「企业账号积分由租户管理」错误（预期内，不炸面板）
- [ ] 个人版 TraeWork 账号行为完全不变（签到/积分/续期照旧）

## 6. 已知残余风险

1. **ExchangeToken 多余字段**：企业分支已改为最小 body `{RefreshToken}`（与官方前端一致），此风险已消除。
2. **企业 CA 证书**：Go on Windows 用系统证书库，公司机器信任 CA 即可；若报 `x509: certificate signed by unknown authority`，把公司 CA 根证书装入系统信任库。
3. **企业 refreshToken 有效期**：若短于个人版，保活调度（keepalive）应能覆盖；观察一周日志确认。
