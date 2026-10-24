# BPS 两条路径第二轮复核（2026-10-12）

复核基点：首轮独立集成 `e7b94bbc301776215bf844776bab2a434e1033d9`。本轮是用户要求的追加审查与修复；来源仍固定为 ranxi `1a74ed0e3cc6cf6923ba4747e8147decf15556b0`，没有同步其他 fork 功能。改动属于三个品牌共享能力。

检查对象为客户端 HTTP/JSON/SSE → BPS，以及客户端 WS → 现有 ingress owner → BPS HTTP/SSE → WS。同时核对最终转换成 Responses 的 Messages/Chat 入口。对照本项目 [Responses 恢复契约](../OPENAI_RESPONSES_RECOVERY.md)、[WS 可靠性方案](../audit/2026-10-08-ws-reliability-plan.md)及当前实现，追踪入口、调度、转发、转换、会话、失败、用量和下一轮请求；没有以协议包单测通过替代入口审核。

## 发现并修正

| 问题 | 触发和影响 | 修正与回归证据 |
| --- | --- | --- |
| 首次调度漏传模型 | 默认调度路径检查原生 WS 模式，BPS 账号 mode=off 在首帧被拒；之前只测试转发层未暴露 | 调度兼容性带入请求模型和全局开关；真实 handler 的 off/ctx_pool 双轮测试通过；非 BPS 模型与总开关关闭仍遵循原生 WS 规则 |
| 后续轮次账号刷新 | Refresh 仍要求原生 WS 开启，并未识别部分 BPS Extra 更新 | 采用渠道映射后的模型复核 BPS 资格，更新现有快照；BPS 资格变化要求携完整历史重连，不暗中切原生协议 |
| 独立桥接互相抢占 | BPS 账号设置为 ctx_pool/off 时，同会话新连接可能取消旧连接 | BPS 与现有 HTTP bridge 一样按连接持有 HTTP 请求和历史；不登记本地/分布式抢占 owner。原生 ctx_pool 抢占回归保持通过 |
| HTTP request ID 被当成 response ID | response.id 尚未出现时取消，错误使用 x-request-id 生成取消终态并推进历史锚点 | 仅观察到的真实 response.id 可作取消/续接/绑定身份；无 ID 返回带取消 event_id/stream_id 的请求错误，保留最后已确认历史 |
| 终态与取消竞争 | 已交付 completed 后出现取消/EOF，可能误判失败或发第二个终态 | 交付终态后结束读取；owner 记录终态已发送，取消不覆盖它；测试在写 completed 的回调中立即取消上下文 |
| 工具修复中断漏记用量 | 被验证层暂扣的工具响应已报告 10/2，纠错又报告 7/3 后 EOF/取消，下游仅看见部分或零用量 | 复用纠错解析器保留进行中 usage；转换器停止并 join 后发布实际用量快照。JSON/SSE/WS 矩阵确认 17/5、仅两次上游调用、不重放已接受生成。无可见 response ID 时仅生成内部账单 ID |
| 自动 compact 后限流漏账 | compact 已消耗 7/3，随后生成 HTTP 429；handler 把可切换错误一概当成零消耗 | Responses/Messages/Chat/WS 在切换前记录非空的实际结果；第二账号 compact+生成记录 10/4。7 个入口变体核对固定 BPS URL、账号切换、两笔不同 ID 且无重复记录 |
| Messages/Chat 漏接 BPS | 两个兼容入口自行构造原生上游请求，未经过 Forward 的 BPS 分流；Excel 主授权可能被发往原生 Codex | 在现有入口选 BPS；复用 Forward 的授权、预算、工具修复、计费和现有 apicompat 转换。Excel-only 未满足 BPS 条件时显式拒绝。映射仅执行一次；EOF 产生协议错误，不伪造 message_stop/[DONE] |
| 不透明上下文静默丢弃 | conversation/prompt 或畸形 previous_response_id 被请求白名单过滤，模型丢上下文 | 明确拒绝不能展开的服务端上下文；WS 已确认的本地历史仍由 owner 展开 |
| 原生映射与策略旁路 | BPS 关闭后，原生 compact 仍被账号 BPS 模型分支抢先处理；BPS 提前分流也绕过 Fast 禁止策略 | 原生 Forward 显式使用原生 compact 映射；渠道检查尊重全局 BPS 开关；BPS 共用现有 Fast 准入。兼容请求转换后应用既有 effort 策略，不在原生 Responses/WS 路径重复映射 effort |

## 复核覆盖与实现约束

| 层面 | 本轮核对内容 | 主要验证载体 |
| --- | --- | --- |
| 入站与调度 | 全局/账号/模型三重条件、渠道和账号映射、首次/后续轮次、原生 WS off、BPS/原生切换、兼容入口资格 | `openai_excel_bps_models_test.go`、`openai_gateway_ws_group_pricing_test.go`、`openai_excel_bps_compat_test.go` |
| WS 协议与历史 | 单 reader、串行输出、租户及响应归属、取消事件关联、无 ID 取消后续接、工具往返、预热、独立连接、已交付终态 | `openai_ws_bps_ingress_test.go`、`openai_excel_bps_reliability_test.go`、现有全部匹配 OpenAIWS 的回归 |
| 时间和资源 | 总尝试预算、首语义输出期限、元数据/心跳不延长期限、上下游关闭、纠错 reader 取消、转换 goroutine 退出 | reliability 测试、完整 basispoints 与 transportdiag 竞态检查 |
| 工具及上下文 | namespace/custom/function、工具参数与批次身份、有界纠错、不执行/重放无授权操作、structured output、不透明服务端上下文拒绝 | 完整 basispoints 单测与竞态；既有工具与结构化输出用例 |
| 费用与失败 | 实际报告 usage、compact/repair 累加、EOF/取消保留用量、切账号前费用、账单 ID 与响应归属分开、失败不伪装成功 | `openai_excel_bps_failover_test.go`：真实 handler + 进程内 Redis 协议服务；WS handler 实际异步记录测试 |
| 授权、图片、安全与原生回归 | 独立 grant/CAS/401/撤销、403/429 分治、图像 relay/附件/compact、可信 IP、原生 WS/Grok/Fable、原迁移与品牌 | BPS/Excel 专项、后端完整 unit；这些边界沿用首轮实现，未恢复退役扩展 |

新增的兼容辅助函数有明确必要性：原 Messages/Chat 转发器同时管理原生授权、HTTP 请求和重试，直接复用会再次绕过 BPS；故仅复用它们已有的 apicompat 转换函数，并共用现有 SSE 帧适配 writer。没有另写工具转换器、重试器或 WS 状态机。已删除旧的重复纠错响应解析代码。

Fast 策略沿用本地准入/过滤规则；BPS 支持哪些调度等级由 BPS 协议决定，不承诺原生 priority 或原生 WS 的加速效果。没有增加新的全局开关。

## 验证记录

使用 Go 1.27.2；以下均为本轮执行。

- 专项单测通过：`go test -tags=unit -timeout 90s ./internal/service ./internal/handler -run 'BPS|Excel'`；service 2.767 秒、handler 0.099 秒。
- 扩大 WS 竞态通过（service 54.429 秒、handler 11.391 秒）：`go test -race -tags=unit -timeout 180s ./internal/service ./internal/handler -run 'BPS|Excel|OpenAIWS|OpenAIResponsesWebSocket'`。
- 完整协议与传输竞态通过：`go test -race -timeout 150s ./internal/service/basispoints ./internal/util/transportdiag`。
- 全量后端复跑通过：`go test -tags=unit -timeout 300s ./...`（service 227.975 秒，其余包通过或复用相同代码的成功缓存）。
- 关键无 ID 取消、工具纠错 EOF/取消、终态后取消、compact 限流入账和 WS 中断账单用例使用 `-race -count=20` 连续通过；service 2.182 秒、handler 5.920 秒。
- `go build -o /tmp/sub2api-bps-reviewed-server ./cmd/server` 通过。
- 只读同步脚本通过：固定来源与 production 差异仍是 Channel Monitor 相关候选；282 项本地散列无漂移。`git diff --check` 通过。
- 本轮业务 Go 代码 **+445/-136 行，17 文件**；测试 **+539/-27 行，9 文件**。另有文档、清单和一行文档追踪白名单变更。

全量 unit 的首次运行有一个未改动的 `TestRecordCyberPolicyEvent_RuntimeSnapshotRefreshFailureKeepsStaleScope` 在 1 秒内未满足计数断言；其余包和本轮 BPS 用例通过。该用例在当前代码和 `e7b94bbc3` 的只读 Go overlay 基线各单独连续 20 次通过。保留首次失败记录，不通过修改无关测试隐藏结果；随后完整重跑通过；不把首次失败描述为从未发生。

首批新用例在修复前复现了无 ID 取消、纠错用量、WS 账号刷新及不透明上下文问题。进一步接入真实 handler 后，又暴露首次调度和兼容入口漏接；上述问题不是仅据代码风格推断。

## 仍需真实环境验收

没有使用真实 Excel grant 或付费 BPS 请求；未运行生产迁移、部署、发版或推送。进程内 Redis 测试覆盖协议与缓存交互，但不能证明真实 Redis 多实例、PostgreSQL 行锁或线上故障切换；Docker daemon 保持原状态。上线前仍需测试环境验收真实模型权限、长时间生成、图像视觉内容、外网 relay、断网及授权刷新。

本轮只修改后端和审核资料；前端、品牌资源、数据库结构和发布渠道未改变。新增 HTTP method/path **0**、配置项 **0**、主传输路径 **0**；在已有 Messages/Chat URL 内补齐 **2 个 BPS 分流点**，共用原 HTTP/SSE 路径。

来源清单保留首轮所有记录，并扩展到 **282 文件**。本轮修改的协议文件标为本地适配；后续与 ranxi 同步必须检查这些差异，不能按来源整文件覆盖。详见 [行为决定](upstream-changes.md)与 [integration-state.json](integration-state.json)。

## KDAN 错误反馈补充复核（2026-10-12）

基点为已发布的单一 BPS 提交 `5a2a1c2dd187805244de4545f394312bde60750c`。用户提供的样例分别为 `basispoints_model_access_changed` HTTP 403 和流式 `server_error` 500；账号启用 BPS 或选中模型并不授予 Excel endpoint 的权限。500 的现有诊断仅含网关摘要，不能据此确认 BPS 内部出错的原因，事件序号 3 也不能证明已有语义内容交付。

代码复核与回归先复现了模型权限错误未交回账号切换的缺口。本次修正四个具体问题：

1. HTTP 403 / 流内明确模型拒绝冷却当前账号的实际模型，再尝试其他可用账号；全池拒绝仍返回模型权限错误，不自动关闭整个账号。Messages 保留 403/permission_error；WS 全池拒绝保留模型原因并按现有耗尽契约以 1013 关闭，不误报客户端认证失败。
2. HTTP 临时 5xx 与未提交内容的已识别流内临时失败复用原有安全恢复；有界暂存生命周期元数据，已交付内容、取消、未知执行副作用、加密状态拒绝仍阻止透明重放。加密签名校验拒绝即使附带 503 也不进入临时故障重试。
3. WS 固定账号在后续转发前也核对模型冷却；新旧模型发现响应都过滤已拒绝模型，不修改共享缓存对象或增加每次推理的发现调用。
4. 真实流错误在转换前独立保存限定字段，转换器停止后复用现有脱敏方法写入 Ops。保留真实 HTTP 状态和上游请求 ID，客户端不收到原始错误或凭据。

新回归覆盖 7 个客户端入口变体 × 5 类 HTTP/SSE 权限及 500 失败，以及 7 个入口 × 2 类账号池耗尽，共 49 个场景，验证账号切换/耗尽、完整输入不变、无失败 ID 泄漏、上游端点仍为 BPS。流内失败尝试的 7/2 用量与成功账号的 3/1 分别入账且不重复；全池失败时两个已报告用量的尝试各记 7/2，HTTP 拒绝不捏造用量。其他专项核对已交付文本不重放、未知托管副作用不重放、取消不重放、显式 403 优先于不一致的 server_error 代码、实际诊断保留、缓存隔离及绝对期限不被错误终态清空。

没有新增 HTTP 接口、配置项、数据库变更、后台循环或主传输路径。现有转换接口无法同时提供安全客户端消息和原始诊断，因此只在现有 streamBody 增加独立失败快照与一个内部读取函数；不重复实现 SSE 读取或错误脱敏。

本次验证使用 Go 1.27.2，命令均在 backend 执行：

- 完整后端 unit 通过：`go test -tags=unit -timeout 300s ./...`；service 229.459 秒，handler 46.576 秒。随后增加的耗尽错误映射和加密签名保护由下列最终专项及竞态复核覆盖。
- 最终 BPS/Excel 专项通过：`go test -tags=unit -timeout 90s ./internal/service ./internal/handler -run 'BPS|Excel' -count=1`；service 2.745 秒、handler 0.149 秒。
- 最终扩大 WS 竞态通过：`go test -race -tags=unit -timeout 180s ./internal/service ./internal/handler -run 'BPS|Excel|OpenAIWS|OpenAIResponsesWebSocket' -count=1`。
- 完整 basispoints 与 transportdiag 竞态通过：`go test -race -timeout 150s ./internal/service/basispoints ./internal/util/transportdiag`；basispoints 16.203 秒，transportdiag 成功缓存复用。
- 49 个入口故障/耗尽场景、安全重放阻断、期限、无 ID 取消、取消归属、模型权限及缓存隔离用例采用 `-race -count=20` 通过；service 30.309 秒、handler 8.280 秒。
- 最终 `go build -o /tmp/sub2api-bps-runtime-errors-server ./cmd/server` 与 `git diff --check` 通过。
- 只读来源检查固定基准仍为 `1a74ed0e3`；本地 production 最新观察为 `c82a856fc`，BPS 核心转发/协议/模型权限文件没有新差异。共享接线新增涉及 Astra 打票、Mihomo 静态代理及 Channel Monitor，未纳入本修复；282 项清单散列与当前本地代码一致。
- 本次业务 Go 代码 **+252/-51 行，9 文件**；测试 **+378/-15 行，13 文件**。HTTP 接口 **0**、配置项 **0**、主传输路径 **0**。

本轮未使用真实 Excel 凭据，也未更新线上进程。真实 BPS 500 的根因应从修正后的上游事件中继续核实；故障恢复测试通过不等于上游模型权限或服务可用性恢复。

### 错误修复代码的再次复核（2026-10-12）

以当前未提交的错误修复差异为对象，复核 HTTP/SSE 和客户端 WS 桥接。新增或扩展回归在修正前复现了以下四处遗漏，均在已有处理链内修正：

1. **可恢复模型 403 提前停止首输出期限。** 原生事件分类会将模型拒绝视为输出；BPS 在决定换账号前就清除了整轮绝对期限。错误/失败/取消终态改为先按 BPS 规则判断是否恢复，再决定停止期限；成功终态的原语义不变。`TestExcelBPSRetryableFailurePreservesTurnDeadline` 同时覆盖 server_error、basispoints_model_access_changed 和 model_not_allowed。
2. **原始诊断晚于 Ops 快照。** 仅在返回时写入诊断已经晚于每轮首次冻结的错误快照，最终落盘仍可能只有摘要。现在在非恢复终态冻结 Ops 之前关闭并 join 转换器、记录脱敏诊断；返回时复用同一逻辑且不重复记录。扩展 `TestExcelBPS500RecoverySafetyAndOriginalDiagnostics` 核对实际 Ops 快照，而不只核对请求上下文。
3. **工具纠错失败关联到首次请求 ID。** 原始错误可能来自后续纠错请求，诊断却使用首次响应头。现在独立跟踪诊断所属的响应，转换器 join 后读取；账单请求 ID 和用量累计保持原契约。`TestExcelBPSRepairFailureDiagnosticKeepsRequestIdentity` 覆盖 JSON、SSE、WS，并核对 17/4 累计用量。
4. **SSE 心跳后全池失败仍按 200 落盘。** 耗尽分支原本未标记应用失败，也未保留 Ops 的稳定错误码。现在模型权限和临时上游故障耗尽都使用已有失败标记，保留逻辑 403/5xx 和原因；`TestExcelBPSFailoverExhaustedAfterKeepaliveRecordsFailure` 验证 wire 200 与逻辑失败的区分。

本次复核没有引入新接口、配置项、主传输路径或独立重试机制；仍不恢复票据、自动登录或代理治理。

全部修正后的完整后端 unit 通过：`go test -tags=unit -timeout 300s ./...`，service 227.968 秒、handler 46.958 秒。最终扩大 BPS/WS 竞态检查通过：`go test -race -tags=unit -timeout 180s ./internal/service ./internal/handler -run 'BPS|Excel|OpenAIWS|OpenAIResponsesWebSocket' -count=1`，service 54.862 秒、handler 11.673 秒。前三项回归连同取消归属以 `-race -count=20` 通过（service 18.516 秒）；耗尽监控回归另行 `-race -count=20` 通过（handler 1.117 秒）。最终服务端构建、gofmt 检查、`git diff --check` 及 282 项来源清单散列检查均通过。

相对已发布基点 `5a2a1c2dd`，错误修复加本次复核累计业务 Go 代码 **+273/-54 行，9 文件**；测试 **+463/-15 行，13 文件**。复核后仍未提交、推送或部署。

## 工具目录错误 #13928 补充复核（2026-10-12）

基点为合并此前修复并已推送的单一 BPS 提交 `4d848ca6d5584b12ebfec69b7da489e44a75e51d`。用户在 KDAN 提供的事件为流式 `basispoints_protocol_error` 502，消息 `basispoints returned a tool outside the client's catalog`。该消息由网关转换器产生；旧诊断没有拒绝名称和实际 HTTP 状态的独立证据，不能认定就是上游 HTTP 502，也不能唯一确定原事件的工具目标。

本轮确认并修复两项缺口：

1. **封装形式改变了已声明目标的可用性。** 直接调用可识别宿主 `functions.` 前缀，BPS JSON 信封及部分原始文本标记却不能。修复前的四项回归复现拒绝；现在 FUNCTION、CUSTOM、FUNCTION_CODE、FUNCTION_CMD、字面封装恢复和格式纠错共用同一个目录查询。真实完整名称优先，只剥离一层显示前缀；真实 namespace、不猜目标、批次验证及原操作保持规则不变。
2. **本地目录失败缺少受限诊断。** 未知工具错误进入既有独立失败快照，在 Ops 首次冻结前经现有脱敏器保存结构证据。日志区分 HTTP 接受状态与本地语义 502；客户端保持安全错误，实际已报告用量仍记账。不记录参数、schema、完整目录或用户内容，尊重原错误正文日志开关。

回归覆盖 JSON/SSE/WS 三种客户端形式 × 六类目录/日志/隐私场景（18 个场景）；同时扩展可见输出后禁止再生成、再生成最多一次、已有工具历史禁止重放、namespace 权威性、操作保持与缓存一致性检查。诊断会注明是否已再生成，但不能从旧事件的 token 数或事件序号倒推尝试次数。请求模型与响应模型不一致的样例保留两个真实字段；线上原因仍须核对账号模型映射和上游记录，不能将改写响应字段当作修复。

本次属于三个品牌共享的协议修复，未改变品牌资源、数据库或发布渠道。未调用真实 Excel 凭据或生产请求，未部署线上服务。

验证记录（Go 1.27.2）：

- 扩大 BPS/WS 竞态通过：`go test -race -tags=unit -timeout 180s ./internal/service ./internal/handler -run 'BPS|Excel|OpenAIWS|OpenAIResponsesWebSocket' -count=1`；service 53.262 秒、handler 11.610 秒。
- 完整协议与传输竞态通过：`go test -race -timeout 150s ./internal/service/basispoints ./internal/util/transportdiag -count=1`；basispoints 12.295 秒、transportdiag 1.052 秒。
- 新增诊断矩阵、单次再生成、可见输出边界、前缀/namespace 及操作保持回归以 `-race -count=20` 通过；service 22.217 秒、basispoints 3.354 秒。
- 全量后端 unit 最终通过：`go test -tags=unit -timeout 300s ./...`。本轮首次执行 service 228.056 秒、handler 50.737 秒均通过；修改测试断言后全量复跑成功，basispoints 2.233 秒，其余相同代码的包复用成功缓存。
- 首次完整协议竞态和并行全量 unit 检查暴露 CMD 的旧契约测试仍要求拒绝一层宿主前缀。已按统一解析规则调整 CODE/CMD 的名称断言，继续拒绝重复前缀、其他命名空间、近似 marker、错误工具类型及非法参数；没有删除该安全测试。上述最终通过记录为调整后的复跑结果。
- 服务端构建、gofmt、`git diff --check` 通过；282 项来源清单更新本地适配散列并通过只读检查。来源仍固定 `1a74ed0e3`，只读核对本地 source checkout 的 `c82a856fc`，本轮没有 fetch 或导入其他来源变更。

相对 `4d848ca6d`，业务 Go 代码 **+69/-22 行，8 文件**；测试 **+172/-6 行，7 文件**。新增 HTTP 接口 **0**、配置项 **0**、主传输路径 **0**，复用现有目录转换和 Ops 记录。

## 过载错误 #13949 补充复核（2026-10-12）

本轮继续保留 #13928 的未提交修复。新事件明确记录真实上游 HTTP 200，之后 SSE 返回 `server_is_overloaded` / `service_unavailable_error`，客户端收到的 502 是网关语义分类结果。与上一条缺少目标信息的工具错误不同，本条已经具备定位分类遗漏的原始证据；不能仅凭 `sequence_number=2` 判断原请求是否已交付内容或满足重试条件。

根因在 `basispoints/upstream_failure.go` 的精确标识表：原来支持 `overloaded_error` / `service_unavailable`，没有覆盖本条 BPS 的两个标识。唯一业务逻辑修改是在既有 503 分类中补齐它们，复用原 Forward/handler 安全恢复；没有扩展未知错误、自由文本匹配、重试次数或账号切换预算。

先补回归再修改业务代码，修复前复现了 503 被归为 502、安全账号切换和期限保留未进入预期分支。回归在既有测试矩阵中新增 4 类过载场景 × 7 个入口（28 个场景），覆盖无显式数值状态的 `error` / `response.failed`、只有 error.type 的情况，以及全池过载。核对首次失败用量与后续成功/失败各自入账、两次请求使用不同账号、原历史与固定 BPS endpoint 保持不变；WS 耗尽仍以 1013 关闭并记录逻辑 503。

同时扩展原分类、安全恢复及期限测试：已交付文本、未知托管工具副作用、取消终态、显式 403、加密拒绝和未知错误均不因过载处理而突破原边界；真实 HTTP 200、上游请求 ID、脱敏详情和实际用量保留。没有用真实 Excel 凭据调用上游，不能保证真实 BPS 容量已经恢复。

本轮相对 #13928 修复的增量：业务 Go 代码 **+1/-1 行，1 文件**；测试 **+39/-10 行，4 文件**。新增 HTTP 接口、配置项、主传输路径均为 **0**。归属为三个品牌共享协议能力，目前仍为本地未提交改动，未推送、部署或发版。

本轮验证（Go 1.27.2）：

- 原分类表上的新增回归先失败，补齐标识后定向单测通过：协议包 0.775 秒、service 0.145 秒、handler 0.137 秒。
- 完整协议及传输竞态通过：`go test -race -timeout 150s ./internal/service/basispoints ./internal/util/transportdiag -count=1`；13.663 秒 / 1.032 秒。
- 扩大 BPS/WS 竞态通过：`go test -race -tags=unit -timeout 180s ./internal/service ./internal/handler -run 'BPS|Excel|OpenAIWS|OpenAIResponsesWebSocket' -count=1`；service 52.851 秒、handler 11.637 秒。
- 过载入口与恢复边界连续 `-race -count=20` 通过，service 15.202 秒、handler 5.208 秒；整轮首输出期限测试单独重复 20 次通过，service 1.277 秒。
- 服务端构建、gofmt、`git diff --check` 及 282 项清单散列核对通过。上一轮工具修复仍在此次协议和 BPS/WS 回归覆盖内；本轮没有重新执行整个后端的 `./...` unit。
