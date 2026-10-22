# sub4api（huoji120）Claude / Codex 优化覆盖复核 —— 钉版

复核日期：2026-10-19（Asia/Shanghai）。本文替代 2026-10-18 的 `sub4api-current-coverage-2026-10-18.md`；那份报告写完后工作区又变动了 63 个文件，其行号引用已大面积失效。

## 1. 钉点与本地验证能力

| 项 | 值 |
|---|---|
| 被审对象 | `/Users/luhonglin/Git/sub2api` 工作区快照，**tree `6259bcbb9f96d6f83722cd7527c30cc40b506001`**（含未提交与未跟踪源码；HEAD `e80eb7ca0`） |
| 快照方式 | 临时索引 `git add -A` + `git write-tree`，`git archive` 导出到 `/tmp/sub2api-pinned/`；全部证据与回归在导出副本上执行，不受并行编辑影响 |
| 来源 | `huoji120/sub4api` 公开 `main` = `a3eaf63afc672ffedf7844e6cfc5cb6750d52a3e`（2026-10-18 已宣布转私有，本文只覆盖该公开快照） |
| 本地路径约定 | 不写前缀的路径均相对 `backend/internal/`；来源路径相对其仓库 `backend/internal/` |

**本地验证能力声明（两项本地做不了）：**

- `golangci-lint 2.13.1`（built with go1.27.0）对 go1.27.2 的 export data 报 `version 5 > maximum supported version 4`，整包 typecheck 失败；对未改动的包同样复现。**lint 未验证。**
- `go test -race ./internal/service -run Codex` 中 `TestNewConfiguredCodexModelDescriptorUsesProviderMetadataAndSafeFallback` 为**预存在失败**（把本轮 Codex 改动 stash 回基线后 3/3 复现，单独运行通过）。**-race 基线本身是红的。**

**钉版回归（Go 1.27.2，`-tags=unit`，导出副本）：**

```text
gofmt -l internal/                       → clean
go vet ./internal/{service,handler,repository}   → 0
internal/handler     ok   91.2s
internal/repository  ok   12.0s
internal/service     FAIL 252.2s —— 唯一失败：
  TestOpenAIGatewayService_Forward_NormalizesMaxTokensAndStripsPromptCacheOptions
    /max_tokens_归一化为_max_output_tokens_并移除_prompt_cache_options
    openai_gateway_service_hotpath_test.go:229  "Should be false"
```

对照：同一时段在**活动工作区**直接跑是 service 5 个、handler 1 个失败，gofmt 脏文件名在两次检查之间变化 —— 并行会话正在编辑。钉版比活动树干净得多，说明并行工作在收口，但未完成。

**未执行**：真实供应商请求、抓包、集成（PostgreSQL/Redis）套件、性能测量、线上账单核对。「官方行为」一律是对来源代码注释中 codex-rs 引用的转述，未独立核验 codex-rs。

## 2. 计分板

判定枚举：**有**（行为等价，已接入生产调用链）· **部分**（主体有、有明确残留）· **等效**（不同实现达到同一效果）· **刻意不同**（有理由地不照搬）· **缺**（来源有可达实现、本地没有、且无记录的理由）· **待决策**（是否要做本身是产品/策略决定）。

| | 有 | 部分 | 等效 | 刻意不同 | 缺 | 待决策 | 合计 |
|---|---|---|---|---|---|---|---|
| Claude | 12 | 2 | 1 | 2 | 0 | 0 | 17 |
| Codex | 14 | 2 | 1 | 2 | 3 | 3 | 25 |
| **合计** | **26** | **4** | **2** | **4** | **3** | **3** | **42** |

「缺」只剩 3 项且全在 Codex；「待决策」3 项需要你拍板，不是工程缺口。与 10-18 报告相比，当时判「缺/未覆盖」的 13 项现已在树里（见 §7）。

## 3. Claude 对照

| # | sub4api 行为（a3eaf63a） | 本地判定 | 证据（tree 6259bcb） |
|---|---|---|---|
| C1 | CLI 基线 2.1.283 + Windows/x64/node v22.19.0 画像；只接受合法 `claude-cli` UA 写入账号 Stainless 画像 | **刻意不同** | 本地 2.1.301，从官方 linux-arm64 二进制采样：`pkg/claude/constants.go:85`；UA 校验 `service/identity_service.go:59` |
| C2 | billing 块固定 `cch=00000`（配置标 no-op） | **刻意不同** | 本地在最终 body/endpoint 确定后签名：`service/gateway_billing_header.go:164 finalizeClaudeCCH` |
| C3 | billing 指纹按 JS UTF-16 下标采样；跳过 isMeta；system 迁移前捕获原始 user 文本 | **有** | `service/gateway_billing_block.go:33,43`（utf16）、`:58`（isMeta）；`service/gateway_billing_header.go:27 captureClaudeBillingSource` |
| C4 | 真实 CC 缺 billing 块时补齐 | **有** | `service/gateway_billing_header.go:66 ensureClaudeCodeBillingBlock` |
| C5 | `metadata.user_id` 保留未知字段（tk 等）；`parent_session_id` 与 session 同命名空间映射 | **有** | `service/metadata_userid.go:27 ExtraFields`；`service/identity_service.go:444` |
| C6 | session masking 改账号级随机命名空间种子，Redis Lua 原子创建，父子同映射 | **有** | `service/identity_service.go:431`；`repository/identity_cache.go:62-77`（`getOrCreateSessionMaskSeedScript`，PEXPIRE 续期） |
| C7 | 六个控制头（usage-limit / prev-tool-durations / compacted ×2 / compaction ×2）透传 + 从上游响应观测入 Redis，仅 mimic 补缺 | **有** | `service/gateway_claude_code_headers.go:14`；接入 `service/gateway_upstream_request.go:223,388`、`service/gateway_count_tokens.go:612` |
| C8 | `count_tokens` 与 Messages 保持客户端识别、beta、system、provider 路由一致 | **部分** | 控制头与 CCH 已接入（C7）；「非 CC 调用的 system/billing 注入」未逐项核对 |
| C9 | Vertex service-account 的计数走 `count-tokens:rawPredict` | **有** | `service/vertex_service_account.go:448 action := "rawPredict"`；分流 `service/gateway_count_tokens.go:475`。10-18 报告判「明确缺口」已过时 |
| C10 | Anthropic SSE 按空行聚合，多条 `data:` 换行拼接，保留 id/retry/注释；EOF 补合成边界 | **有**（EOF 刻意不同，见 §5） | `service/gateway_sse_event.go:49 parseAnthropicSSEEvent`、`:12 anthropicSSEEventBuffer`（整事件有界）；EOF 不补：`service/gateway_upstream_response.go:779` |
| C11 | API-key passthrough 同样事件级解析，终态需有 data | **有** | `service/gateway_anthropic_passthrough.go:557` |
| C12 | 请求重建保留大整数（UseNumber） | **有** | `service/gateway_request.go:51 decodeJSONUseNumber`，4 处重建 + SSE 1 处 |
| C13 | 工具名回译只还原本请求实际改写的名字 | **有** | `service/gateway_tool_rewrite.go:333`（注释标明 adapted from sub4api） |
| C14 | `max_tokens=1` 探测拦截不限 Haiku，返回 `#` + `max_tokens` 终止，流式/非流式都有 | **部分** | 拦截已任意模型 + 流式分支：`handler/gateway_handler.go:2312`；**残留**：`:217 isMaxTokensOneHaikuRequest` 仍只对 Haiku 设置 ClaudeCodeValidator 绕过标志 —— 拦截和绕过是两个门 |
| C15 | body session 与 `X-Claude-Code-Session-Id` 头同步 | **有** | `service/gateway_upstream_request.go:233,1024` |
| C16 | `/api/event_logging/batch` 遥测只回 200 不转发 | **等效** | `server/routes/common.go:17`，与来源完全相同（同上游起源） |
| C17 | dateline 隐写指纹清除 | **有** | `service/gateway_claude_oauth_body.go:1291`（上游 2026-07 即有） |

## 4. Codex 对照

| # | sub4api 行为（a3eaf63a） | 本地判定 | 证据（tree 6259bcb） |
|---|---|---|---|
| X1 | 原生 WS 客户端、连接池、WSv2 透传、WS→HTTP 大包桥 | **有** | 上游 2026-02～06 起源；`service/openai_ws_client.go:300` 等。来源与官方基线 3040209f2 逐字节相同 |
| X2 | 普通 HTTP 入站的 OAuth 账号按账号 WS 决策走上游 WS，网关仍回 SSE/JSON；API-key relay 保持 HTTP；compact 保持 unary HTTP | **有** | `service/openai_gateway_forward.go:143 http_relay_preserved`、`:148 compact_http`；测试 `openai_ws_forwarder_v2_test.go:18`。10-18 报告判「未覆盖」已过时 |
| X3 | 只对 `https://chatgpt.com/backend-api/codex/responses` 的 JSON POST 做 zstd 请求压缩，GetBody/ContentLength 一致 | **有** | `repository/http_upstream.go:339` 发送点调用 |
| X4 | HTTP 头、`client_metadata`、内嵌 turn-metadata 三处会话身份统一；缺 namespace 不外发 | **有**（方案 B，与来源 A 刻意不同，见 §5） | `service/openai_codex_account_identity.go:139 applyCodexOfficialSessionHeaders`、`:183` namespace 守卫 |
| X5 | 最终出站头削平：删 `session_id`/`conversation_id`/`x-codex-installation-id`/`accept-language`；HTTP 另删 `OpenAI-Beta` | **有**（HTTP 侧 OpenAI-Beta 处理刻意不同，见 §5；**文档矛盾**见 §7） | `service/openai_codex_identity.go:256`；**8 个调用点**覆盖 HTTP 四路径 + WS 四路径：`openai_gateway_forward.go:1578`、`openai_gateway_passthrough.go:852`、`openai_gateway_chat_completions.go:371`、`openai_gateway_messages.go:414`、`openai_ws_forwarder_v2.go:182`、`openai_ws_forwarder_ingress.go:1080,1173`、`openai_ws_v2_passthrough_adapter.go:962`。带 namespace + 已有 `session-id` 守卫，不会丢隔离 |
| X6 | turn-metadata 复刻 serde 声明序 + flatten 字典序、嵌套形状、不转义 `&<>`、非 ASCII 小写 `\uXXXX` | **有**（DEL 与 `\b\f` 刻意不同，见 §5） | `service/openai_codex_turn_metadata.go:33 codexTurnMetadataKeyOrder`、`:224` DEL 取舍注释 |
| X7 | `client_metadata` 缺省项合成：`window_number`/`context_window_id`/`turn_started_at_unix_ms`/`request_kind` | **有** | `service/openai_codex_turn_metadata.go:502-508` |
| X8 | 完整 `tool_namespaces_info` 留在帧内，从 HTTP/WS 头里剥离 | **有** | `service/openai_codex_turn_metadata.go:525` |
| X9 | WS 握手透传 `x-codex-parent-thread-id`/`x-openai-subagent`/`x-openai-memgen-request`/`x-responsesapi-include-timing-metrics` | **有** | `service/openai_ws_forwarder_payload.go:121` |
| X10 | session/thread/turn 用确定性 **UUIDv7**（时间戳锚定过去 200 天）；window 保持 `thread:ordinal`；caller 纳入缺省 thread 派生 | **刻意不同** | 本地保留 `deriveStableUUIDv4`：`service/openai_codex_fingerprint.go:215`；无 v7 派生函数。理由见 §5 |
| X11 | 多条重建路径恢复顶层键序 | **有** | `service/openai_gateway_upstream_errors.go:555 restoreOpenAIUpstreamJSONTopLevelOrder` |
| X12 | 工具别名 `python__sub2api` → `python__custom` | **有** | `service/openai_codex_tool_names.go:17` |
| X13 | 旧 `/responses/compact` 主动提升为原生 compaction v2（`PromoteOpenAILegacyCompactRequest`） | **待决策** | 本地无该函数；只识别客户端原生 `compaction_trigger`。这是行为改变，不是补头 |
| X14 | OAuth 401 时刷新后最多恢复一次（HTTP + 三个 WS 入口），永久拒绝按凭据版本记忆 | **等效** | `service/gateway_turn_recovery.go:96 refreshRejectedCredential`，HTTP/passthrough/WS 共用；无同名 `openai_oauth_recovery.go` 不等于未实现 |
| X15 | 按 attempt 的 `forward_timing`（连接获取/发送/首字节/首语义/首 flush） | **有** | `service/openai_forward_timing.go:18` |
| X16 | `ensureCodexResponsesDefaults`：body 级补 `reasoning:{}`、`tool_choice:"auto"`、`parallel_tool_calls`、`include:["reasoning.encrypted_content"]` | **缺** | 本地只有 `ensureCodexReasoningInclude`（`service/openai_codex_transform.go`），无其余三项。与 X7（client_metadata 层）是两层，10-18 报告未区分 |
| X17 | Codex OAuth/SetupToken 的 WS 握手固定 `responses_websockets=2026-02-06`，不降级 v1 | **缺** | `service/openai_ws_forwarder_payload.go:174` 仍对 ResponsesWebsocket 传输无条件降 V1，无 Codex lane 守卫 |
| X18 | WS 服务端错误事件套用账号 `temp_unschedulable_rules`（来源 `25b06c394`） | **缺**（未找到） | `openai_ws_forwarder_v2.go`/`_ingress.go`/`_v2_passthrough_adapter.go` 中无 `TempUnschedulable`/`HandleUpstreamError` 引用；若经共享错误路径处理，本文未追到 |
| X19 | 账号级请求时区：改写 `environment_context` 的 `timezone`、重算/补齐 `current_date` | **待决策** | 本地无 `openai_request_locale.go`，`environment_context` 只出现在探测模板；`FORK_FEATURE_RETIREMENT.md` 未将其列为退役。属 MACOS-DO 时期功能，是否要是产品决定 |
| X20 | 出站身份三元组同源：originator 配 UA 首段、version ≥ 0.144.0、自定义 UA 只贡献 OS/arch、凭据面不发 version | **有** | `service/openai_codex_identity.go:16 codexUpstreamMinVersion`、`:91 CodexCanonicalAuthIdentity` |
| X21 | turn-state 客户端回带优先、票据 Cookie 只留 Cloudflare/`__oailb` | **刻意不同** | 打票体系已按 `FORK_FEATURE_RETIREMENT.md` 退役，Cookie 白名单无承载对象；跨账号 turn-state 剥离守卫保留：`service/openai_codex_turn_state.go:122` |
| X22 | Responses Lite：工具与 instructions 转输入前缀，禁并行 | **有** | `service/openai_responses_lite_tools.go:25`（09-26 合并即有） |
| X23 | 取消 `server_is_overloaded` 的请求级早退，让账号 `temp_unschedulable_rules` 照常命中写模型级冷却 | **待决策** | 本地**明确反向设计**：`service/openai_account_runtime_block_fastpath.go:106` 注释「Capacity shedding describes this request, not account health. Keep the account schedulable」。两边各有理由，是策略选择 |
| X24 | HTTP Responses 等待上游响应头及首个完整事件期间的下游保活 | **部分** | compact 有：`service/openai_compact_sse_keepalive.go:24`；普通 Responses 路径只找到 `openai_first_output_timeout.go:520` 的超时/暂存 guard，**未找到**首字节前的下游心跳写入 |
| X25 | ModelTrace 降智检测（号原生）+ 自动降智检测（默认关） | **部分** | ModelTrace 保留（`service/modeltrace_challenge.go:55`，含许可证）；自动检测无 `codex_auto_degradation_check`，属退役运营能力 → 恢复与否是待决策 |

## 5. 刻意不同步清单（防止被「同步」回来）

| 项 | 来源做法 | 本地做法 | 理由 |
|---|---|---|---|
| SSE EOF 边界 | reader goroutine 在干净 EOF 补发一个空行，派发残留事件 | **不补**（`gateway_upstream_response.go:779`） | 来源注释「Scanner 吞掉尾空行」经实测**错误**：`"a\nb\n\n"` → `ScanLines` 产出 `["a","b",""]`。补边界的真实效果是把截断残片当完整事件派发，残片形如 `message_stop` 时会置位终态、压掉本该发生的重试 |
| CCH | 固定 `cch=00000` | `finalizeClaudeCCH` 在最终 body/endpoint 后签名 | 本地更接近 2.1.301 真实行为，不退回占位值 |
| CLI 画像 | 2.1.283 Windows/x64 | 2.1.301 linux-arm64（官方二进制采样） | 更新；不复制作者个人机器画像 |
| 身份迁移形状 | A：把 16-hex 隔离值直接写进 `session-id` | B：header 复用 body 已作用域化的 UUID | A 造出「`session-id` 不是 UUID」且保留两套派生；B 让 header/body 首次同源。见 `CODEX_SESSION_IDENTITY_MIGRATION.md` §5.1 四条约束 |
| `thread-id` 缺失 | 用 `session-id` 补 `thread-id` | 不补 | 不把所有 thread 等同于 session |
| UUID 版本 | v7 时间戳锚定 | 保持 v4 | 改派生函数会同时改 body 身份与 `prompt_cache_key` 派生（kind 判断耦合），本轮不动 |
| turn-metadata DEL / `\b` `\f` | DEL 原样、`\b` `\f` 短形式 | DEL 转义（HTTP 头安全）、`\u0008`/`\u000c` | 有测试守住 header-safe；控制符不会出现在实际内容里，不为此加未验证分支 |
| HTTP 侧 `OpenAI-Beta` | 整头删除 | 只删 `responses_websockets=*` 与 `responses=experimental`，保留其它 beta（如原生 compaction） | 传输协商与身份迁移独立 |
| 打票 / Cookie 回放 / 自动停调 | 全套 | 退役 | `FORK_FEATURE_RETIREMENT.md` |
| 自动把全量 input 裁成严格后缀增量 | **来源公开版也没有**（`openAIWSInputIsPrefixExtended` 无生产调用） | 同 | 不能当作本地落后 |

## 6. 待决策清单（需要你拍板，不是工程缺口）

| # | 事项 | 影响 | 建议 |
|---|---|---|---|
| D1 | X19 请求时区 / `environment_context` 日期补齐 | 改写客户端环境块内容；属「地区模拟」范畴 | 若不做，写进 `FORK_FEATURE_RETIREMENT.md` 明示退役，避免反复讨论 |
| D2 | X13 旧 compact 主动提升为 compaction v2 | 行为改变，影响 `/responses/compact` 老客户端 | 先量有多少流量走旧 compact 再定 |
| D3 | X23 overloaded 是否触发账号级冷却 | 调度策略：请求级重试 vs 账号级摘号 | 本地现有设计有注释依据；若改，需同时改 `RequestScopedTransient` 语义 |
| D4 | X16 `ensureCodexResponsesDefaults` | 纯特征收敛，调用方显式值优先，风险低 | 建议做，与 X7 配套 |
| D5 | X17 WS Codex lane 固定 beta v2 | 一行条件；影响仅 OAuth/SetupToken 握手 | 建议做，需确认 API-key 路径不受影响 |
| D6 | X25 自动降智检测恢复 | 运营能力，默认关 | 按退役文档口径决定 |
| D7 | X24 普通 Responses 首字节前下游心跳 | 长 TTFT 场景下代理/CF 断连 | 先确认是否已由 `openai_first_output_timeout` 以其它方式覆盖 |

## 7. 流程偏差与文档矛盾（本次新发现）

1. **削平已在两条 lane 上执行，但迁移文档说没执行。** `projectCodexOutboundHeaders` 有 8 个调用点（§4 X5），而工作区 `docs/CODEX_SESSION_IDENTITY_MIGRATION.md:182-183` 仍写「第一阶段仍保留 session_id、conversation_id…删头不在本批执行」。原定顺序是「补齐 → 隔离验收 → 删」；代码已到第三步，文档停在第一步。需要补 §8.3 的验收记录，或者确认第二步验收确实做过（§8.3 提到「两个 API Key、两份账号 namespace…」测试，可能已覆盖，但没有和削平动作关联起来）。
2. **两份关键文档被 `.gitignore` 忽略。** `.gitignore` 的 `docs/*` 规则 + 白名单：并行会话把 `CODEX_RESPONSES_TRANSPORT_OPTIMIZATION.md`、`TEST_DATA_AND_ENVIRONMENT_SIMULATION.md` 加进了白名单，但 `CODEX_SESSION_IDENTITY_MIGRATION.md` 与 `SSE_STREAM_ERROR_USAGE_ACCOUNTING.md` **没有**（`git check-ignore` 均为 ignored），因此不在钉版 tree 里、也不会随任何 commit 走。`TRANSPORT_OPTIMIZATION.md` §11.1 链接到的「身份迁移实施更新」在仓库视角是不存在的。**本文同样被忽略**，需要白名单一行才能入库。
3. **C14 残留**：`handler/gateway_handler.go:217` 的 Haiku-only validator 绕过与 `:2312` 的任意模型拦截不一致 —— 非 Haiku 的 `max_tokens=1` 请求会被拦截返回 `#`，但不会获得 validator 绕过标志。需确认这是否刻意。
4. **10-18 报告的 13 项「缺」已过时**：C3 isMeta、C9 Vertex、C10 error 帧字段、C11 passthrough 事件解析、C12 UseNumber、C13 回译守卫、C14 流式分支、C6 masking 种子、C7 控制头、X2 HTTP→WS、X3 zstd、X5 削平、X15 forward_timing。其 24 条本地行号引用抽查至少 10 条指到空行或无关行。该报告不应再作为状态依据。

## 8. 核验边界

- 判定依据是钉版 tree 的源码阅读 + 一次单元回归；「有」表示函数存在且在生产调用链上被引用，不表示已对真实上游验证。
- 「官方行为」均转述自来源注释，未读 codex-rs；来源作者的「1:1 / 不降智 / 不封号」是作者声明，本文不为其背书，也不把字段差异推断为风控结果。
- X18、X24 的「缺 / 未找到」是 grep 级结论，若由共享路径覆盖需人工追认。
- 未查 `usage_logs` 或账单，SSE 错误用量入账（`SSE_STREAM_ERROR_USAGE_ACCOUNTING.md`）的线上规模未知。

## 9. 建议顺序

1. 等并行会话收口：让 `TestOpenAIGatewayService_Forward_NormalizesMaxTokensAndStripsPromptCacheOptions` 转绿，`.gitignore` 白名单补齐两份迁移/计费文档，迁移文档 §8 补削平验收记录。
2. 处理 §6 的 D4、D5（低风险、纯特征），其余 D 项先决策。
3. 修 C14 残留（一行条件）。
4. 重新钉 tree、重跑回归、以本文结构更新计分板。
