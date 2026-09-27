# ranxi2001 打票需求与适配记录

来源：`ranxi2001/sub2api` 的 `production`，核对到 `cdb2d7a427023185090a4040a3a0e73c6cccf84d`（v2.8.18，2026-09-27）。本项目基线：`b3475dabc`（0.2.8.4）。归属：公共能力，适用于公共、KDAN、TapModels。

交付前再次核对远端跟踪分支 `f80611d6c`：相较上述来源新增的是 BPS、凭证守护与 Windows 时区修复，打票/调度/轮次准入文件没有新增差异；这些范围外功能没有引入。

本次按用户确认的范围适配出口/会话绑定、Cookie 时效、轮次准入及打票调度，继续使用现有代理池和 ModelTrace。没有整分支 merge：该分支同时包含已明确移除的非打票功能。轮次准入使用 `cherry-pick --no-commit -x` 移植三个原提交，再与本地票据格式和生命周期合并；提交说明保留来源。其它打票改进在现有实现中适配，不恢复 ranxi 的整套打票子系统。

## 需求、来源与代码

| 需求 | ranxi 来源 | 本项目实现 |
| --- | --- | --- |
| 每次探针使用新会话，成功后业务复用该会话 | `c23cf4923`，`codex_harvest_probe.go` | 现有 `codex_probe_template.go` / `codex_replay_request.go` 已生成新的 session/window/turn；新增 `codex_ticket_binding.go` 保存实际探针身份并复用 |
| 票据与打票出口绑定，出口不可用时禁止漂移 | `docs/codex-ticket-pinning.md`，`openai_codex_ticket_egress.go` | 保存现有代理池的代理 ID、URL 摘要；真实发送前校验启用状态、到期时间和摘要。HTTP、SSE、WS 均走该出口，不回退账号代理或直连 |
| Cookie 与票据一起复用，过期后停止发送，不被失败响应续期 | `8d2d73094`、`3719ff895`、`f69b68154` | `codex_ticket_binding.go`；绑定凭证最长 240 秒，上游 Cookie 声明更早失效时使用更早时间；业务响应不延长凭证有效期。同名 Cookie 不再追加混用 |
| 打票期间保护业务，限制后台并行请求 | 打票设计文档、并行打票相关提交 `f69b68154` | `codex_ticket_activity.go`；业务优先，本进程开始业务会取消该账号的后台探针，流结束释放占用。打票前和落库前也检查共享并发计数。复用现有每账号/模型数据库锁、并发上限、总超时和失败随机退避 |
| 每一轮发送前核验账号、分组、限流、模型票据和连接绑定 | `e16bb28dd`、`f2d52e904`、`ea041d209`（PR #23 及修复） | `service/openai_turn_admission.go`、`repository/openai_turn_admission.go` 和各转发入口。主库只读可重复读事务获取一致快照；查询失败拒绝发送，不写账号/代理健康惩罚 |
| WS 的票据必须是实际握手代次，续轮不得复用已退休代次 | 轮次准入提交、`openai_codex_ticket_egress.go` | 以本地 `GenerationID` 校验，保留已有握手观测；连接池额外按代次和 API Key/会话隔离。换代使用新连接；已开始的响应可完成，下一轮重新检查 |
| 失效处理不得自动重放已发送的请求或误删新票 | `openai_codex_ticket_feedback.go` | 保留本地发送快照和数据库按代次 CAS 失效审计；继续按“turn-state 与 __oailb 同时变化”证据失效。不以返回状态长度判断模型质量，不自动重放已计费请求 |
| 手动打票、模型参与、代理选择、节奏与历史可观测 | ranxi 打票控制台、`codex_harvest_controls.go` / `openai_codex_ticket_manual.go` | 复用现有账号打票面板、代理池、参与开关、间隔设置和审计流水；增加凭证到期时间，并修订刷新与出口说明 |

核心入口位于 `backend/internal/service/`，管理界面为 `frontend/src/components/admin/account/CodexTicketDashboard.vue`。票据原文、身份字段和 Cookie 仍在服务端账号 extra 中，沿用既有敏感字段过滤；代理凭证只通过代理仓库解析，不另存进票据。

## 适配取舍

- ModelTrace 指纹与完整 `response.completed` 仍是入库条件；长度为 292、312 或 780 以及 `response.created` 自报模型均不能替代验票。
- 按实际打票请求复用身份。本项目探针带有 `prompt_cache_key` 和 `client_metadata`，因此保存并复用这些值；没有照搬 ranxi 无条件删除它们的行为。未出现在探针中的身份字段会被去掉，业务输入与工具正文保留。
- 继续使用本地一个账号/模型一个当前代次的设计，没有增加 ranxi 的 standby 票据。新代次不得被旧响应的失效事件删除。
- 轮次准入保留原实现的 OpenAI 路径覆盖，包含 Responses、Messages、Chat Completions、Images、WS 原生/直通以及 HTTP 桥接。非 OpenAI 平台保持原路径；simple 模式遵循原有跨分组调度规则。
- Mihomo 托管节点、订阅/Selector 管理、节点记忆与国家筛选，以及 780 定向铸造、edge IP、指定 unified 网关，按用户确认本轮不引入。相关来源包括 `9b57ba927`、`943980a06`、`b4b4be949`。需要这些能力时应独立评估出口生命周期、凭证验证及运维配置。
- ranxi 的独立批量打票控制台和全局流水页不另建一套；现有入口承担相同类型的操作和审计。本轮不恢复 BPS/Excel、质量运营、观察员、DeepSeek/Copilot 等非打票功能。

## 配置与升级行为

1. 设置 → Codex 打票：继续使用现有总开关、无票策略、重试间隔、主动刷新间隔和探针模板。账号 → 打票：选择账号与模型参与、查看状态/流水、执行手动打票。IP 设置 → Codex 打票代理池：选择打票出口。
2. 新捕获的绑定凭证最长有效 4 分钟，会在到期前 30 秒尝试更新。更短的主动刷新间隔仍生效；`0` 只关闭额外的定时刷新，不能让过期凭证继续使用。业务繁忙时刷新可能推迟，无票策略禁止时，到期后的新请求会被拒绝，等待重新打票。探针频率可能高于旧版本默认的 30 分钟，需按账号数量和上游消耗设置参与范围。
3. 旧 ModelTrace 票据继续兼容原行为；下一次自动或手动成功打票后具备完整绑定信息。升级后可手动刷新需要立即启用绑定保护的账号/模型。不把旧票据伪装成已知出口的绑定票据。
4. 代理出口绑定到代理 ID 和配置，不是实际出口 IP 的证明。代理服务若在同一 URL 后轮换 IP，本项目无法保证真实 IP 不变，应配置固定出口。
5. 主库查询和代理查询在请求发送前执行，带超时。数据库暂不可用时轮次准入会拒绝请求；它不提供数据库事务与外部网络发送之间的跨系统原子性。
6. 本进程业务/打票协调是互斥保护；跨实例使用共享并发计数的前后检查，仍存在检查到提交之间的竞争窗口。各实例票据更新和失效继续依赖数据库代次 CAS，不能用这些检查宣称全局零竞争。
7. 不新增迁移，不修改历史 SQL，不执行生产数据库操作。绑定元数据使用现有票据 JSON 字段；新增到期状态使用已有 API 字段。
8. WS 续轮也重新校验绑定代理。关闭全局打票后，带有票据的旧连接需重连，重新握手时不再携带绑定票据；当前已开始的响应可以完成。`session.update` 中的身份字段不能覆盖探针身份，模型和业务指令仍按原协议处理。

## 验证记录

2026-09-27 在本机执行：

- 后端全量：`go test -tags=unit ./...` 通过。重点覆盖 HTTP/SSE、原生 WS、直通 WS、HTTP 桥接，以及在途完成后下一轮遇到账号/票据过期的拒绝行为。
- 数据库：Docker 中 PostgreSQL 18.1、Redis 8.4；`CI=true go test -v -tags=integration ./internal/repository -run 'Test(OpenAITurnAdmission|CodexTicket)' -count=1` 通过，25 个顶层测试、0 跳过。测试库执行现有迁移，验证主库快照、并发入库、回滚、锁与代次 CAS；未接触生产数据库。
- 竞态：`go test -race ./internal/service -run 'Test.*(CodexTicket|TurnAdmission|HTTPBridge.*Admission|Passthrough.*Admission)' -count=1` 通过。涵盖业务取消后台探针、流结束释放占用、固定出口与会话、过期/换代、代理不可用、服务重启后提前刷新和关闭打票后的连接退休。
- 后端静态检查：`golangci-lint run --new-from-rev=b3475dabc`，新增问题 0。
- 前端：类型检查、ESLint、繁体生成检查、生产构建通过；Vitest 全量 361 个文件、2797 个用例通过，包含中/英/繁/日语言键与插值一致性。

验证范围为本机模拟上游、数据库与并发测试；没有使用真实上游账号，不能据此断言外部服务当前一定接受这些票据。未做生产负载下的数据库延迟压测。
