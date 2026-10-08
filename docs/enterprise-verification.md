# 企业版 Trae 接入 — Phase 2 真实账号验证清单

> Phase 1 代码改造已完成（`feat/enterprise-trae` 分支）。本清单需在**公司网络内 + 真实企业账号**下按序执行，
> 对应调研纪要 §6 的待验证项。每项给出预期结果与不一致时的处理分支。

## 0. 准备

```bash
go build -o dist/wild-work.exe ./cmd/wild-work && ./dist/wild-work.exe
# 打开面板 http://127.0.0.1:7863，点「＋ Trae 企业版」
```

## 1. client_id 校验（纪要 §6.1）

**步骤**：默认配置（官方 solo id `en1oxy7wnw8j9n`）走一遍登录；若失败，改 `config.json`：

```json
"enterprise_trae": { "console_host": "https://trae.comnova.cc", "client_id": "ono9krqynydwx5" }
```

重启后再试。

- **预期 A**：官方 id 直接通过 → 什么都不用改。
- **预期 B**：官方 id 被拒、企业 CLI id 通过 → 配置保留企业 id（已支持，零代码改动）。

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

## 3. chat 兼容性（纪要 §6.3）

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
