# ranxi2001/sub2api 2.8.13 功能移植记录

日期：2026-09-26。来源：本地克隆的 ranxi2001/sub2api `production`（`3e345632f`，v2.8.13）加 `codex/bps-tool-probe`（`1dfbc6f4d`，Excel BPS 账号工具往返探针）。
基线：`4d615a091`（本仓库已合并 sub4api 1.1.5）。此次仅本地移植，不推送、不发布镜像、不部署、不操作生产数据库。

## 合并方式

ranxi2001 是基于较旧上游（`efe9aab1e`）的独立 fork，上游提交多以 cherry-pick 方式重放（与本仓库真实合并的上游提交 SHA 不同），并且自带一套与本仓库互斥的 Codex 打票体系。整体 `git merge` 会产生 109 个冲突文件，且无法单独剔除打票体系，因此按用户 2026-09-25 的决定：

- 从基线建整合分支 `integrate/ranxi2001-2.8.13`，按功能 `cherry-pick`（PR 合并提交用 `-m 1` 取净变化），每个功能一个提交，提交说明列出来源 PR/SHA，作者保留原作者；
- 不带入 ranxi 的上游重放提交（本仓库已通过真实合并拥有上游代码）；
- 功能专属文件与 ranxi 最终代码逐字节一致（`basispoints/`、`openai_excel_bps*.go`、`requestcapture/` 等已核对），差异只在与本仓库结构交汇处。

## 已移植功能

| 提交 | 功能 | 要点 | 迁移 |
| --- | --- | --- | --- |
| DeepSeek × Codex | Responses Lite 走 chat 回退 | 按主机/平台/映射后的 deepseek-* 模型识别 DeepSeek（含中转站）；命名空间工具与 custom_tool_call 历史展平；剔除 json_schema response_format；DeepSeek 远端压缩 v2 生成 Codex 兼容事件；推理与回答分离；补 Codex 必需的 usage 字段 | — |
| OpenAI 传输加固 | TLS 重试、代理运行时回退、SSE 生命周期 | 仅在未发出任何 HTTP 字节时对瞬时 TLS 握手失败重试一次；发送前连接失败按代理已有的 `FallbackMode/BackupProxyID` 回退（直连或备用代理，最多 4 跳）；透传 SSE 读泵、客户端断开后继续排空用量 | — |
| Cyber 会话屏蔽 | 按明确会话身份屏蔽 | 只用明确的 thread/conversation/session ID（按 API Key 隔离）建立本地屏蔽，移除基于对话相似度的判断；身份冲突/无效不写入；可选严格模式（默认关） | — |
| 账号分组计费倍率 | `group_rate_multiplier` | 与用户/API Key 的分组倍率相乘，默认 1.0；账号编辑与批量编辑可改 | 241_add_account_group_rate_multiplier |
| banana 图片模型 | `/v1/images` | banana* 按 Gemini 兼容图片模型处理 | — |
| 插件快照 | 隐私修复 | 插件账号快照不再带出打票私有 extra | — |
| 请求耗时明细 / TPS | 管理员用量抽屉 | 采集请求各阶段耗时与物理上游尝试；健康色带与解读；平均 TPS；弹窗拖选文字松手在遮罩上不再误关 | 242_request_timing_details |
| 鹈鹕测智 | 管理员 HTML 测试 | 最多 8 路并行生成 HTML 供人工比较；服务端定时运行（复用定时测试计划）；记录看板 | 243_pelican_scheduled_tests |
| 分组模型策略 | 三项 | 账号在各分组可用模型（只收窄）；用户在分组内禁用模型；分组仅允许流式请求 | 244_account_group_allowed_models、245_user_group_denied_models、246_group_stream_only |
| 公告指定用户 | 公告定向 | 只对选中的用户可见，支持弹窗提醒 | — |
| 质量运营 / 账号告警 / 鹈鹕展示 | 智能运营菜单 | 定时答题 + 采纳模型判分，只在明确判错时移出分组或停调，可自动恢复并报告冲突；上游明确余额不足/周额度耗尽时邮件告警；用户侧鹈鹕展示页（默认关） | 247、248、249_account_ops_alerts、249_pelican_showcase_items |
| Copilot SDK | OpenAI API Key 账号的 sidecar 模式 | 保留原生 Codex 工具、断开取消回合、不重放有状态回合；`deploy/copilot-sdk` 配套脚本与 `docs/copilot-sdk-codex.md` | — |
| 凭证守护 | 智能运营子页面 | 巡检 OpenAI OAuth 账号 access_token、可选自动重登、错误态自愈、Bark 通知 | 250_account_token_guard |
| Excel / BPS | OpenAI OAuth 账号开关 | 选定模型（或全部）经 ChatGPT Excel BPS 上游转发；工具协议转换与回放、结构化输出校验、缓存创建计为普通输入（可选）、base64 图片中转与入口资源准入、批量设置、账号测试含工具往返探针；BPS 不支持的请求回退 Codex 通道 | — |
| 请求采集 | 管理员定向采集 | 按用户/上游账号/入口分组限时采集，只保留确认失败的请求或 WS 回合，鉴权字段写盘前脱敏，默认关 | 251_request_captures |
| 测试 | Gemini keepalive | 用 `testing/synctest` 消除时序抖动 | — |

## 未移植的内容

- **ranxi 打票体系（约 70 个提交）**：Mihomo 托管出口/订阅/国家过滤/动态代理、打票流水页与手动/并行采集、票据钉住出口与 Cookie、780 原生采票、分组打票范围与优先级、fail-open 界面、业务版座位识别、热唤醒等。本仓库保留 sub4api 打票体系（见 `SUB4API_MERGE_1_1_4.md`），两者核心设计互斥（TTL 票 vs 代际失效）。对应迁移 `239_codex_harvest_node_learning`、`240_codex_harvest_flow_events` 未引入。
- **发送前轮次准入（ranxi PR #23，turn admission）**：它对每次上游发送从主库重读账号并在库不可用时拒绝，WebSocket 绑定逻辑基于 ranxi 的票据模型（TTL、备用票、身份匹配）。移植需要按 sub4api 票据模型重写，且会给热路径增加一次主库读取，因此暂不引入；依赖它的测试断言相应去掉。
- fork 自身的版本号、发版流程与 CI、README/社群、更新渠道（`check owner production releases`）、鹈鹕/耗时的试验构建工作流。

## 与本项目的适配

- **两套 BPS 并存**：sub4api 的 `openai_bps` 是独立平台与独立账号；本次的 Excel / BPS 是 OpenAI OAuth 账号上的开关，界面名称分别为 “OpenAI BPS” 与 “Excel / BPS 协议”，代码与会话状态互不共享。
- **与 sub4api 打票体系的衔接**：Excel BPS 路由的模型不参与门票（账号判定、出站模型、门控、状态列表都会跳过）；代理回退时门票观测仍包在最终响应外；BPS 组合目录按请求分组的账号模型限制筛选。
- **构造与接线**：网关服务新增代理仓库依赖（`ProvideOpenAIGatewayService` 同步加参数）；新增的 handler/服务手工接入 `wire_gen.go`。
- **API Key 鉴权快照版本 26**：缓存分组新增 `stream_only` 与用户禁用模型，叠加本项目已有的兜底分组、Codex 清单与 model_allowlist 字段。
- **安全调整**：凭证守护在来源分支预置了第三方测活/重登服务作为默认值（启用后会把账号 access_token，自动重登时还有邮箱、密码、2FA 密钥发给该服务）。本项目去掉该默认值、自动重登默认关闭，启用前必须由管理员填写可信地址。
- **文案**：界面中写死的 “Sub2API” 改为 `@:{'common.siteName'}`；补齐全部日文；繁体由 `tools/zh-tw/gen-locale.mjs` 生成；凭证守护页写死的中文表头移入语言包；发给模型的鹈鹕/糖果题提示词保持中文，并加入 zh-TW UI 审计白名单。
- **文档**：`cyber-session-blocking`、`account-quality-operations`、`pelican-scheduled-tests`、`copilot-sdk-codex`、`account-token-guard`、`excel-bps`、`request-capture` 加入 `.gitignore` 白名单；Copilot 文档改为从本仓库复制脚本，不再克隆 fork。

## 升级时必须知道的行为

1. 新增迁移：241、242、243、244、245（用户分组禁用模型）、246、247、248、249（两份）、250、251。全部为新增列/表且带 `IF NOT EXISTS`，与本仓库已有的 239–245 编号并存（迁移按文件名记账）。只做了 SQL 审阅与单元测试，未在真实 PostgreSQL 上执行。
2. **代理回退会实际生效**：原本只用于“代理到期改绑”的 `FallbackMode`，现在也用于 OpenAI HTTP 发送前的连接失败。配置了“直连”回退的代理，在连接失败时会改为直连发送（仅限确认请求尚未发出）。
3. **Cyber 屏蔽口径变化**：没有明确会话 ID 的请求不再被本地屏蔽，交给上游审核。
4. **DeepSeek 路由变化**：映射到 DeepSeek（包括中转站上的 deepseek-* 模型）的 Codex Responses Lite 请求改走 chat 回退。
5. **请求耗时明细常开**：每个计费请求会异步写一行 `request_timing_details`（单行上限 128 KiB，队列满时丢弃），保留 30 天，每小时最多清理 1 万行；高流量站点请关注表增长。
6. 默认关闭的新功能：图片中转与入口准入、请求采集、凭证守护、鹈鹕展示、账号告警、Cyber 严格模式、分组仅流式；账号分组计费倍率默认 1.0。
7. Excel BPS 使用账号已有的 ChatGPT 凭据；图片中转需要公网 HTTPS 地址，并把 `/api/bps-images/` 转发到同一实例。

## 验证记录

- 每个功能提交分别通过 `go build`、`go vet -tags=unit` 与相关包测试；前端 `vue-tsc`、语言包完整性与相关组件测试。
- 最终整合树：`go test -tags=unit ./...` 全量通过；`golangci-lint run --new-from-rev=4d615a091` 0 issues；`go generate ./ent` 零差异；`vue-tsc --noEmit`、`vitest run` 全量通过；`node tools/zh-tw/gen-locale.mjs --check` 与 zh-TW UI 审计通过。
- 未运行集成测试（本机无可用的 PostgreSQL/Redis 容器），未使用真实上游账号、真实 BPS 或 Copilot sidecar 验证。
