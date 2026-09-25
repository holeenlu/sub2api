# sub4api 1.1.5 实际代码合并记录

日期：2026-09-25。来源：本地 sub4api 的 `v1.1.5`（`main` = `ca30f2141bc9ced4cbcfa2ef336f795a94c133e2`，Merge PR #18 release/1.1.5）。
公共基线：`f7f8309f9`（已含 sub4api 1.1.4，见 `SUB4API_MERGE_1_1_4.md`）。与 1.1.4 相同，使用普通 Git merge，保留来源提交历史；此次仅本地合并，不推送、不发布镜像、不部署、不操作生产数据库。

1.1.4 之后 sub4api 只新增两个提交，内容全部是 OpenAI BPS：

- `200de9c98` feat(bps): add OpenAI BPS support for 1.1.5
- `775744a66` fix(bps): show credential failures and confirm account risks

## 已合并功能

| 功能 | 实际行为 | 主要位置 |
| --- | --- | --- |
| OpenAI BPS 独立平台 | 平台值 `openai_bps`、账号类型 `oauth`（界面显示 Access Token）；上游固定 `https://bps.openai.com/basispoints/api/responses`，服务端设置 Bearer、两个账号 ID 头与 `x-basispoints-auth-mode: chatgpt`，不转发客户端认证头 | `backend/internal/service/openai_bps_*.go`、`backend/internal/server/routes/openai_bps.go` |
| 凭证管理 | 手动粘贴 Access Token；JWT 中自动提取 `chatgpt_account_id` 与到期时间（只解析不验签），可手动覆盖账号 ID；编辑留空保留原 Token；替换 Token 不解除管理员主动停用 | `openai_bps_credentials.go`、`OpenAIBPSAccountFields.vue` |
| 凭证状态 | 区分未知/未到期/已过期/已撤销/认证失败；状态栏、用量列、编辑窗口统一显示，自然过期每 30 秒刷新；认证失败只按本次请求的 AT 与账号 ID 条件写入，迟到响应不影响新凭证；管理接口新增只读 `bps_credential_state` | `openai_bps_credential_state.go`、`account_repo_bps_credentials.go`、`BPSCredentialStatus.vue`、`useBPSCredentialState.ts` |
| 调度与路由 | BPS 分组只用 BPS 账号；过期/撤销/认证失败的 BPS 账号不参与调度；Composite 分组需显式 BPS 路由，普通 `gpt-*` 自动识别仍走 OpenAI；只有尚未绑定上下文且尚未输出的新会话才会在网络错误/429/5xx 时有限换号 | `openai_gateway_scheduling.go`、`openai_account_scheduler.go`、`composite_platform.go`、`scheduler_cache.go` |
| Codex 工具协议转换 | function/custom/namespace/additional_tools 声明转成文本目录，经原生 `run_officejs` 承载，再恢复为客户端工具调用；工具仍由客户端执行；支持信封别名、嵌套 JSON、custom 原文、`update_plan` 参数与回执适配；一次最多交付一个工具调用 | `openai_bps_protocol.go`、`openai_bps_envelope.go`、`openai_bps_custom_transport.go` |
| 会话状态 | Redis 保存完整原始工具调用、账号绑定与压缩摘要，跨实例续接，7 天闲置过期；会话按 API Key 隔离，建立上下文后不自动换号重放；缺失上下文返回 `bps_context_expired`，绑定账号不可用返回 `bps_account_unavailable` | `openai_bps_state.go`、`openai_bps_turn.go`、`backend/internal/repository/gateway_cache_bps.go` |
| 压缩 | 支持 `/responses/compact` 与带 `compaction_trigger` 的请求；摘要存 Redis，客户端只拿到 `bpscmp_v1_` 引用；保持用户轮次，工具结果只递增迭代次数，兼容旧摘要与连续压缩 | `openai_bps_turn.go`、`openai_bps_gateway.go` |
| 结构化输出 | `text.format` 的 `json_object`/`json_schema`：提示词约束 + 本地终态校验（大整数精度保留、schema 上限 1 MiB、只允许文档内引用）；校验失败返回 `bps_invalid_structured_output`（未开始响应为 502，已开始 SSE 为 `response.failed`），已取得的 usage 仍记账 | `openai_bps_structured.go` |
| 计费与错误 | 按实际上游 usage 记账，沿用分组/渠道价格、倍率、用户平台配额；`model_not_allowed` 类错误按映射后模型冷却 30 分钟而不停用账号；认证失败停止该账号调度；429 按 `Retry-After` | `openai_bps_gateway.go`、`openai_gateway_forward.go` |
| 模型目录 | 默认候选 `gpt-6-astra`、`gpt-5.6-sol`；标准模型列表格式修正；Composite 目录只发布有合格 BPS 账号承接的模型，无候选返回空列表 | `openai_bps_catalog.go`、`openai_bps_composite_catalog.go` |
| 连接测试 | 创建/编辑窗口「保存并测试连接」；只有收到真实响应才显示 HTTP 状态，保留错误码、上游模型与请求 ID | `openai_bps_test_connection.go`、`AccountTestModal.vue` |
| 风险确认 | 新建 BPS 账号显示封号风险警告；保存与保存并测试都需二次确认，取消不创建且保留输入 | `CreateAccountModal.vue` |
| 渠道监测与 Key 使用说明 | 渠道监测可选 OpenAI BPS（Responses 探活，无配额探测）；API Key 使用说明提供 BPS 分组的 Codex 配置（`wire_api = "responses"`、`supports_websockets = false`） | `channel_monitor_*.go`、`MonitorFormDialog.vue`、`UseKeyModal.vue` |
| 用户平台配额 | 平台配额表加入 `openai_bps`（默认配额 map 由 10 个平台变为 11 个） | `ent/schema/user_platform_quota.go`、`frontend/src/api/admin/settings.ts` |

协议来源与限制见 `docs/openai-bps.md`、`docs/openai-bps-sources.md`（已随本次合并入库并加入 `.gitignore` 白名单）。

## 升级时必须知道的行为

1. 需要执行迁移 `245_openai_bps_platform.sql`（扩展平台约束，不新增凭证表）。**Redis 是 BPS 的必需依赖**，不做进程内降级。
2. BPS Token 需手动替换，不自动续期。首版只支持 HTTP Responses、文本输入与串行工具；不提供 WebSocket、Chat Completions、图像或音频接口；不能用 `previous_response_id`/`item_reference` 代替完整历史。
3. 结构化输出是提示词约束 + 本地校验，不是上游原生约束解码；无效 schema 返回 400，不符合格式的最终响应返回 502 或 SSE 失败事件。
4. 源项目没有真实 BPS token，两模型实际权限与真实 Codex 工具/压缩闭环未验收；上线前请在测试分组用真实账号验证文本、SSE、function/custom/namespace、`update_plan` 及「压缩 → 新一轮 → 工具调用」。

## 保留的本项目差异及适配

- 保留 sub2api Go module 与版本文件（`VERSION` 仍为 0.2.8）；sub4api 新文件中的 `github.com/MACOS-DO/sub4api` 导入改为 `github.com/Wei-Shaw/sub2api`（8 个文件）；不引入 `changelog/` 目录。
- 文档与界面中的「Sub4API」改为中性表述：文档用「本站」，界面提示用 `@:common.siteName`。
- 冲突处理：`IsSchedulable` 保留本项目拆出的 `isSchedulableIgnoringRateLimit`，BPS 凭证失效判定放在其中（限流结束也不会恢复）；账号更新保留本项目的 `mergeCredentials`（编辑与换发两种语义），BPS 账号走 `NormalizeOpenAIBPSCredentials`；调度快照投影同时保留 `anthropic_fable_scheduling_threshold` 与 BPS 需要的 `chatgpt_account_id`/`expires_at`；脱敏键列表加入 `OpenAIBPSCredentialStateExtraKey`；Key 使用说明保留本项目默认模型（`gpt-5.6-sol`、`claude-sonnet-5`）并加入 `openai_bps: gpt-6-astra`。
- 语言包：`bps` 文案块放在 `accounts` 顶部（与来源一致），`codexDiagnosticDialog` 保持本项目位置；补齐日文；繁体由 `tools/zh-tw/gen-locale.mjs` 从简体生成。
- 来源未同步更新的测试：前端平台配额断言由 5 个平台改为 6 个；后端 API 契约的 `default_platform_quotas` 加入 `openai_bps`。
- 为通过本项目 golangci-lint（`errcheck.check-type-assertions: true`）修正 45 处：测试中的未检查类型断言改用 `requireBPSValue[T]` 辅助函数；`strings.Builder` 写入与 `resp.Body.Close()` 显式忽略返回值；`update_plan` 候选循环去掉无效赋值；错误信息改为小写 `malformed BPS SSE event`（ST1005）。不改变判定逻辑。

## 验证记录

- `go build ./...`、`go vet -tags=unit ./...`、`go generate ./ent` 零差异。
- `go test -tags=unit ./...`：全量通过（首轮仅 `TestAPIContracts` 因上述平台配额断言失败，修正后通过）。
- `golangci-lint run --new-from-rev=f7f8309f9`：0 issues。
- 前端：`vue-tsc --noEmit` 通过；`vitest run` 全量通过（首轮 3 个文件失败：两个账号弹窗测试的冲突拼接与平台数断言，已修正）；变更文件 ESLint 通过；`node tools/zh-tw/gen-locale.mjs --check` 通过。
- 未使用真实 BPS 账号，未在真实 PostgreSQL 上执行迁移 245。
