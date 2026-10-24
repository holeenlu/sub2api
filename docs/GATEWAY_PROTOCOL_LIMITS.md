# 网关三路径协议限制矩阵（原生 / Excel BPS / Codex Cloud）

基线：`main`，2026-10-24。来源：[GATEWAY_PATH_CONSOLIDATION_2026-10-24.md](GATEWAY_PATH_CONSOLIDATION_2026-10-24.md) 第 0 节，逐项回到源码核对。路径相对 `backend/`，行号为本文成稿时的位置。BPS 的完整需求与验收见 [docs/bps/REQUIREMENTS.md](bps/REQUIREMENTS.md)，本文不复述。

四列含义：

- **代码硬限制**：写死在代码里的常量，改动需要发版。
- **配置默认值**：`config.yaml`（`deploy/config.example.yaml`）中的键及其代码默认值，改动需要重启。
- **管理设置**：管理后台的系统设置键，运行时生效。
- **上游能力**：上游自身的限制或行为，网关只能适配。

「—」表示该路径没有这一层限制。

## 1. 传输与编码

| 项目 | 路径 | 代码硬限制 | 配置默认值 | 管理设置 | 上游能力 | 位置 |
|---|---|---|---|---|---|---|
| 上游传输 | 原生 | HTTP SSE 或 Responses WS | — | 账号协议开关 | Responses HTTP / WS | `internal/service/openai_gateway_response_handling.go`、`openai_ws_client.go` |
| | BPS | HTTP SSE，经 `basispoints` 翻译 | — | `openai_excel_bps` 账号开关 | BPS Responses 端点 | `internal/service/openai_excel_bps.go`、`basispoints/stream.go` |
| | Cloud | App Server WS JSON-RPC | — | Cloud 账号开关 | Codex Cloud backend WS | `internal/service/openai_cloud_transport.go:19` |
| 出站请求压缩 | 原生 | 仅 `https://chatgpt.com/backend-api/codex/responses` 的 POST 做 zstd | — | — | 接受 zstd | `internal/repository/http_upstream.go:386-458` |
| | BPS / Cloud | 不压缩 | — | — | — | — |
| 上游响应解压 | 原生 | 不由 Go 自动加 `Accept-Encoding`；`decompressResponseBody` 兜底 zstd / gzip / deflate | — | — | — | `internal/repository/http_upstream.go:1620-1624`、`:1781` |
| | BPS | Go 自动 gzip + 同一兜底 | — | — | — | 同上 |
| | Cloud | WS 关闭压缩 | — | — | — | `internal/service/openai_cloud_transport.go:127` |
| 入站请求解压 | 三路径共用 | 解压后上限 64 MiB（`maxDecompressedBodySize`），超出一律 413（见 P1） | — | — | — | `internal/pkg/httputil/body.go:23`、`:208` |

## 2. SSE 与消息大小

| 项目 | 路径 | 代码硬限制 | 配置默认值 | 管理设置 | 上游能力 | 位置 |
|---|---|---|---|---|---|---|
| SSE 行切分 | 原生 | `newSSEScanLines`：CR / LF / CRLF / BOM | — | — | SSE 规范允许三种行尾 | `internal/service/gateway_sse_event.go` `newSSEScanLines` |
| | BPS | 与原生共用 `internal/pkg/sse.ScanLines`（CR / LF / CRLF / BOM）；16 MiB 上限、`event:` 回退、EOF 尾帧不变 | — | — | 同上 | `internal/pkg/sse/sse.go`、`internal/service/basispoints/stream.go` `readEvents` |
| | Cloud | 不适用（JSON-RPC 消息） | — | — | — | — |
| 单行 / 事件上限 | 原生 | 首字前暂存 8 MiB（`openAIFirstOutputStageMaxBytes`） | `gateway.max_line_size`，代码默认 500 MiB（`defaultMaxLineSize`）；示例配置写 40 MiB；0 用默认，非 0 至少 1 MiB | — | — | `internal/config/config.go:2536`、`:3590-3594`；`internal/service/gateway_service.go:44`；`openai_first_output_timeout.go:310`；`deploy/config.example.yaml:519` |
| | BPS | 单行 / 单事件 16 MiB；转换后读泵 16 MiB | — | — | — | `basispoints/stream.go` `readEvents`；`openai_excel_bps.go:1140` |
| | Cloud | 单条 WS 消息 16 MiB；RPC 事件队列 32 MiB / 256 条；事件流存储 16 MiB / 8192 条 | — | — | — | `openai_ws_client.go:23`、`openai_cloud_transport.go:20-22`、`openai_cloud_session_store.go:19-20` |
| 一帧多 JSON | 原生 | 修复，最多 16 份 / 16 MiB | — | — | 上游偶发拼帧 | `internal/service/openai_sse_json_documents.go:14-15` |
| | BPS | 拒绝（严格单值解码是 BPS 契约，不改） | — | — | — | `basispoints/stream.go` |
| | Cloud | 协议错误 | — | — | — | — |
| 下游 SSE 心跳 | 原生 | — | `gateway.stream_keepalive_interval`，默认 10 s；0 禁用，非 0 须 5–30 s | — | — | `config.go:2532`、`:3429-3434`；`openai_gateway_response_handling.go:205-207` |
| | BPS HTTP | — | 同一键、同一语义：默认 10 s，0 或无配置关闭（见 P6） | — | — | `openai_excel_bps.go` `forwardExcelBPS` 心跳段 |
| | BPS WS | 15 s WS keepalive 事件（针对 Codex 五分钟静默，语义不同，不随配置） | — | — | — | `openai_ws_excel_bps_bridge.go:43` |
| | Cloud | — | 同一键；仅 HTTP SSE，WS 桥另有自己的保活 | — | — | `openai_cloud_gateway.go:558-563`、`openai_ws_cloud_bridge.go:26-27` |

## 3. 上下文与密文

| 项目 | 路径 | 代码硬限制 | 配置默认值 | 管理设置 | 上游能力 | 位置 |
|---|---|---|---|---|---|---|
| 上下文窗口 | 原生 | 不预检；模型清单原值透传 | — | — | gpt-6-astra 1,050,000 token | — |
| | BPS | 清单 `context_window` / `max_context_window` 钳到 922,000，`auto_compact_token_limit` 830,000；请求注入 `compact_threshold` 920000 | — | — | 输入约 922k 以上返回 `context_length_exceeded`（1,050,000 窗口减 128,000 输出预留） | `openai_excel_bps_models_manifest.go:10-22`、`basispoints/request.go:260` |
| | Cloud | 协议解析不限请求体（见 P10）；`input` 1–4096 项；owner 在本实例时请求不经 Redis，只受 `gateway.max_body_size`；owner 在另一实例的续接须装进 Redis 命令 `OpenAICloudMaxCommandBytes`（16 MiB，紧凑 JSON 不做 HTML 转义，含账单快照），超出在派发前返回 413 `request_too_large`；每个 actor 会话保留量 16 MiB（新线程计历史与输入，续写只计新消息） | — | — | 真实 codex-cloud-backend 单帧上限未知；网关出站 WS 单帧 16 MiB | `openai_cloud_session.go` `enqueue` / `cloudCommandFits` / `handleCommand`、`openai_cloud_session_store.go` `OpenAICloudMaxCommandBytes`、`openai_cloud_transport.go` `send` |
| 上下文超限分类 | 原生 | `isOpenAIContextWindowError`：code `context_length_exceeded` / `context_too_large`，或消息含 “maximum context length”、“context window … exceed” 等 | — | — | — | `openai_gateway_upstream_errors.go:262` |
| | BPS | HTTP 400 与流内失败都按同一规则归为 `context_length_exceeded`（见 P5）；短语规则唯一归属 `internal/pkg/openai.IsContextWindowErrorText` | — | — | — | `openai_excel_bps.go:940`、`basispoints/upstream_failure.go` `classifyUpstreamFailure`、`internal/pkg/openai/context_window.go` |
| 上下文超限返回形态 | 原生 / 透传 / BPS | 客户端 `stream:true` 且上游以 HTTP 400 拒绝时，改为 HTTP 200 SSE `response.failed`（`response.error.code=context_length_exceeded`，见 P9）；JSON 客户端仍为 HTTP 400；管理员错误透传规则命中时按规则 | — | — | Codex 只从 `response.failed` 识别上下文已满并在下一轮先压缩 | `openai_context_window_failure.go`、`openai_gateway_upstream_errors.go` `handleErrorResponse`、`openai_gateway_passthrough.go` `handleErrorResponsePassthrough`、`openai_excel_bps.go` `fail` |
| 密文 | 原生 | 不可跨账号迁移；400 拒绝时不剥离、不重试 | — | — | 密文绑定签发账号 | `openai_gateway_upstream_errors.go:365` |
| | BPS | 顶层 reasoning 透传；消息密文默认拒绝；推理前 429 且密文自包含时可换号 | — | 账号开关 `openai_excel_bps_ignore_encrypted_content`（有损省略旧消息密文） | 同上 | `account.go:2307`；细节见 [REQUIREMENTS BPS-07](bps/REQUIREMENTS.md#bps-07有界纠错密文兼容与结构化输出) |
| | Cloud | 同账号；历史注入失败时先去 reasoning 密文、再把 compaction 换成提示（有损回退，仅服务端日志可见） | — | — | Cloud 拒绝非其签发的密文 | `openai_cloud_session.go` `injectHistory` |

## 4. 图片与请求体

| 项目 | 路径 | 代码硬限制 | 配置默认值 | 管理设置 | 上游能力 | 位置 |
|---|---|---|---|---|---|---|
| 请求体 | 原生 | 解压后 64 MiB（见第 1 节） | `gateway.max_body_size` 256 MiB；`gateway.text_max_body_size` 32 MiB，用于 `/systemone`、`/alpha/search`、`/embeddings` 三条纯文本路由 | — | — | `config.go:2513-2514`、`server/routes/gateway.go:36-37`、`:209`、`:237`、`:249` |
| | BPS | 图片正文上限不超过 1024 MiB，且不超过 `gateway.max_body_size` | — | `excel_bps_image_body_limit_mib` 默认 64；`excel_bps_image_budget_mib` 默认 1024（512–65536，且至少为正文上限的 8 倍）；`excel_bps_image_max_requests` 默认 128 | — | `setting_excel_bps_image.go:25-37`、`openai_excel_bps_image_admission.go:124` `excelBPSImageMaxBodyBytes`、`basispoints/image_capacity.go` |
| | Cloud | 与原生同受 `gateway.max_body_size`（owner 在本实例，含所有新会话）；owner 在另一实例的续接 16 MiB（Redis 命令，见 P10）；每个 actor 会话保留量 16 MiB、单个上游 WS 帧 16 MiB | — | — | 真实 codex-cloud-backend 入站帧上限未验证 | `openai_cloud_session.go` `enqueue` / `cloudCommandFits`、`openai_cloud_transport.go` `send` |
| 图片 | 原生 | 透传，不校验；multipart 单 part 20 MiB，超出明确报错（见 P2） | — | — | 上游自行校验 | `openai_images.go:38`、`:351-360` |
| | BPS | 单图 64 Mpx；原生附件单图 20 MiB / 请求内联合计 32 MiB | — | relay 模式：`excel_bps_image_max_image_mib` 默认 20、`excel_bps_image_max_total_mib` 默认 32、`excel_bps_image_max_images` 默认 20（原生附件也用这一张数上限） | — | `basispoints/image_relay.go:39-42`、`basispoints/attachments.go:309`、`:320`、`basispoints/image_relay_limits.go:20-26` |
| | Cloud | 只收内联 base64 data URL（协议要求）；无网关单图字节与张数上限（见 P10，与原生一致） | — | — | 不支持远程图片 URL 与 `detail: low`；客户端已缩到 2048 / 6000 px | `openai_cloud_protocol.go` `cloudValidateInlineImage` |

## 5. 方案 P1–P7 的最终行为

- **P1 入站解压溢出（已实现）**：`decompressRequestBodyLimit` 不再区分 `enforce`，总是读 `limit+1`，超出返回 `*http.MaxBytesError{Limit: 64 MiB}`，读取阶段记为 `decoded_size`。`DescribeRequestBodyReadFailure` 先识别 `MaxBytesError`，客户端收到 **413**，消息 `Request body too large, limit is 64MB`，不算客户端放弃。`enforce=false` 仍只表示 identity 编码不校验长度。受影响调用方是走 `ReadRequestBodyWithPrealloc` 的 images、embeddings、alpha/search、gemini、grok media 与复合路由 multipart 分支；以前超限会被截断后交给解析器，表现为 400。
- **P2 multipart 单 part 超限（已实现）**：`parseOpenAIImagesMultipartRequest` 读 20 MiB+1，超出返回 `multipart field <name> exceeds 20 MiB`。handler 对解析错误没有现成的「过大→413」通道，客户端仍收到 **400** `invalid_request_error`，但消息写明超限，不再把截断的图片转发上游。`grok_media.go` 的 `parseGrokMediaMultipartRequest` 同样读 limit+1 并返回该错误，`ParseGrokMediaRequest` 增加 error 返回值，Grok 媒体入口与异步图片任务（提交校验、安全审计）把它按 400 `invalid_request_error` 返回；其余仅用于计费与路由的解析、转发归一化路径同样拒绝，不再转发截断的图片。
- **P3 SSE 行切分共享（已实现）**：新增 `internal/pkg/sse`（唯一导出 `ScanLines`，存在理由是 `basispoints` 不能导入 `service`），`newSSEScanLines` 与 BPS `readEvents` 共用 CR / LF / CRLF / BOM 切分；BPS 的 16 MiB 上限、`event:` 回退、EOF 尾帧和严格单 JSON 解码不变。
- **P4 base64 data URL 解析共享（未实现，按方案前置条件放弃）**：实际只有两处解析器（Cloud `cloudImageBudget.validate` 与 BPS `relayImagePayload`，附件与策略都复用后者），且两者语义不同：relay 拒绝带参数的 MIME（`image/png;name=a`）、区分「非图片类型」与「非法 MIME」两种文案、接受含换行的 base64；Cloud 恰好相反。无损抽取不成立，剩下单一调用方也不构成新包理由，三处均保持原样。
- **P5 BPS 上下文超限分类（已实现）**：HTTP 400 分支改用原生 `isOpenAIContextWindowError(extractUpstreamErrorMessage(raw), raw)`。`basispoints` 不能导入 `service`，流内分类 `classifyUpstreamFailure` 只在 code 不是已知标识时，按同一组短语匹配 code 与 message，命中则归为 `context_length_exceeded`（400）。已知 code 保持原义，匹配到的上游文本只用于分类，不回显。客户端收到 `error.code == "context_length_exceeded"`，不重放，不改账号健康。
- **P6 BPS HTTP 心跳（已实现）**：间隔取 `gateway.stream_keepalive_interval`，与原生和 Cloud HTTP 用同一配置键和同一语义：配置默认 10 s；设为 0 或无配置时关闭下游注释心跳。此前 BPS 硬编码 15 s 且不可关闭。BPS WS 桥的 15 s keepalive 事件语义不同，不变。
- **P9 流式上下文超限改为 `response.failed`（已实现，2026-10-24 用户追加）**：`writeOpenAIContextWindowSSEFailure` 为原生 `handleErrorResponse`、透传 `handleErrorResponsePassthrough` 与 BPS `fail` 共用。客户端 `stream:true`、上游 HTTP 400 且命中上下文超限分类时，先提交 SSE 头与 HTTP 200，再写 `event: response.failed`，`response.error` 保留 `invalid_request_error` / `context_length_exceeded` / 原 `param` / 脱敏消息；Ops 用 `MarkOpsStreamError` 记语义 400，`setOpsUpstreamError` 记上游真实 400。JSON 客户端、已写出的响应、管理员透传规则命中保持原 JSON；上游流内 `response.failed` 本来就原样转发，不变。客户端 `stream` 意图由 `markOpenAIResponsesClientStream` 在 `forwardOpenAIResponses` 与 `forwardExcelBPS` 入口记录（上游线型可能被强制 SSE，不能用上游体判断）。
- **P10 Cloud 图片与请求体对齐原生（已实现，2026-10-24 用户追加，两轮）**：第一轮去掉网关自设的 Cloud 单图 3 MiB 与每消息 8 张上限，去掉 `parseCloudProtocolRequest` / `normalizeCloudResponsesIngress` / `cloudResponseReferences` 的 4 MiB 请求体检查；保留协议要求（只收内联 base64、拒 `file_id` / 远程 URL / `detail: low`、严格 base64 流式校验）。第二轮去掉 Redis 命令封装的天花板：owner 在本实例（所有新会话，以及续接落在 owner 实例）时请求交给 actor 的进程内收件箱（64 + 16 条，与 Redis 命令流同界），不经 Redis；只有 owner 在另一实例的续接才写 Redis 命令，单条上限 `OpenAICloudMaxCommandBytes` 16 MiB（= 每会话未消费命令预算 = 出站单帧），用不做 HTML 转义的紧凑编码，Lua 只解码单独传入的 owner 做归属比较；`cloudCommandFits` 只在该路径派发前预检，超出返回不换号的 413，完整历史命中另一实例的已完成线程而送不过去时改为本实例新开线程。`OpenAICloudMaxRequestBytes` 已删除。方案比较与剩余上限见 `OPENAI_CLOUD_DESIGN.md`「请求体上限对齐原生」；真实 codex-cloud-backend 对大帧的接受情况未验证。
- **P7 Cloud 有损导入可观测性（未实现，按方案前置条件放弃）**：Cloud 成功路径没有可写的 Ops 诊断通道。`openai_cloud_gateway.go` 的 Ops 写入全部在失败分支（`setOpsUpstreamError`、`OpsGatewayResponsesFailedKey`），注入历史发生在无 gin 上下文、可能在另一实例的会话 actor 内，回传的 `OpenAICloudTelemetry` / `OpenAICloudResult` 没有自由文本字段。不新增字段，有损回退仍只在服务端日志（`service.openai_cloud` 前缀）可见。

## 6. 官方 Codex 客户端的硬约定（对照用）

来源：`/Users/luhonglin/Git/codex` @ `970111f6cb`（2026-10-23，`rust-v0.165.0-alpha.1` 之后），路径相对 `codex-rs/`。这些是客户端行为，网关数值要与之兼容而不是复制。

| 主题 | 官方约定 | 位置 | 对网关的含义 |
|---|---|---|---|
| 图片缩放 | 每张图先缩到最长边 2048、≤ 2,500 个 32px patch（detail auto/high）；`original` 为最长边 6000、≤ 10,000 patch（≈ 10.2 Mpx）；PNG/JPEG/WebP 保留原字节，GIF 转 PNG，重编码 JPEG 质量 85；一律 data URL；远程 URL 与 `detail: low` 客户端自己拒绝；无每消息张数上限 | `utils/image/src/lib.rs:24-83,131-137,354-395`、`core/src/image_preparation.rs:36-38` | BPS 单图 20 MiB、64 Mpx 对 high 足够，`original` 的大 PNG 可能超 20 MiB；Cloud 已去掉单图 3 MiB、每消息 8 张与 Redis 命令封装（P10）；Codex `store:false` 每轮重发全部历史图片，Cloud 的 16 MiB 会话保留量/单帧与 BPS 32 MiB 合计会先被打到 |
| 空闲超时 | `stream_idle_timeout_ms` 300,000；只有真实 `data:` 事件 / WS 文本帧重置计时，`:` 注释与 WS ping 不算；超时后按 `stream_max_retries`（5）重试，耗尽后 WS 回退 HTTPS | `model-provider-info/src/lib.rs:68-70`、`codex-api/src/sse/responses.rs:559`、`endpoint/responses_websocket.rs:94-100` | 网关的 `: keepalive` 只对中间代理有用；BPS HTTP 首字超过 5 分钟客户端会放弃并重试，WS 桥的 JSON keepalive 事件才有效 |
| SSE 解析 | `eventsource-stream`，无行长上限；多行 `data:` 以 `\n` 拼接；一帧多 JSON 或 `[DONE]` 静默跳过；缺 `response.completed` 视为流错误并重试；只处理固定事件类型，未知类型忽略 | `codex-api/src/sse/responses.rs:349-492,578-605` | 原生的一帧多 JSON 修复有价值；下游 `data: {json}` 单行形态兼容 |
| 请求压缩 | `Content-Encoding: zstd`（level 3），仅当 provider 名为 OpenAI 且使用 Codex 后端鉴权；不发 `Accept-Encoding`，reqwest 未编译 gzip/brotli | `http-client/src/request.rs:199-223`、`core/src/client.rs:1770-1778` | 入站必须解 zstd；给 Codex 的响应不能压缩 |
| 请求形态 | `store:false`、`stream:true`、`include:["reasoning.encrypted_content"]`、无 `instructions`（developer 消息放 `input` 首项）、Responses Lite 把 tools 放 `input.additional_tools`；function `strict` 恒为 false；`tool_choice:"auto"` | `codex-api/src/common.rs:279-304`、`core/src/client.rs:1016-1127`、`tools/src/responses_api.rs:39` | Cloud 的 `strict:true` 拒绝对 Codex 不触发；`tool_choice` 只会是 auto |
| 上下文与压缩 | `context_window` 缺省取 `max_context_window`；`auto_compact_token_limit = min(清单值, 90% 窗口)`；95% 强制压缩；未知模型回退 272k；token 计数 = 上次 `response.completed.usage.total_tokens` + 本地估算；内置 models.json 里 gpt-6 族 272k/872k，没有 1,050,000 | `protocol/src/openai_models.rs:397-545`、`models-manager/src/model_info.rs:96-147`、`core/src/context_manager/history.rs:939-956` | BPS 钳 922k/830k → Codex 实际在 829.8k 自动压缩、875.9k 强制；网关必须始终转发 usage，否则客户端不会压缩 |
| `context_length_exceeded` | 只在 SSE `response.failed` 里识别为 ContextWindowExceeded（不重试、下一轮先压缩）；HTTP 400 带同一 code 只当 InvalidRequest 显示给用户，不会触发压缩 | `codex-api/src/sse/responses_error.rs:48`、`api_bridge.rs:98-271`、`core/src/session/turn.rs:1824-1827` | 网关对流式请求若以 HTTP 400 返回该 code，Codex 不会自动压缩；以 `response.failed` 返回才会 |
| 远程压缩 v2 | `input` 末尾追加 `{"type":"compaction_trigger"}` POST 到普通 Responses 端点；响应必须恰好一个 `compaction` 输出项 + `response.completed`；流重试 2 次 | `core/src/compact_remote_v2.rs:87-91,419-470` | BPS / 原生的 compaction 输出项数量必须恰好为 1 |
| 错误重试 | HTTP 层重试 5xx 与传输错误 4 次（200 ms × 2ⁿ ±10%），不重试 429；轮次层对 413 / 408 / 499 / 502 / 504 等 `UnexpectedStatus` 重试至多 5 次；400 一律不重试；`response.failed` 里未知 code（含 `invalid_encrypted_content`、`previous_response_not_found`）视为可重试 | `codex-client/src/retry.rs:22-52`、`codex-api/src/api_bridge.rs`、`sse/responses_error.rs:47-101`、`core/src/responses_retry.rs` | 网关返回 413 会被 Codex 重试 5 次再报错；流内 `invalid_encrypted_content` 会被重试 5 次，HTTP 400 形式不会 |
| WS 帧 | tungstenite 默认配置 + permessage-deflate，无自定义 max_message_size；二进制帧报错；ping 在泵内应答不转发 | `endpoint/responses_websocket.rs:576-582,942-943` | 网关入站 WS 读上限 64 MiB 与 tungstenite 默认一致；给 Codex 只能发文本帧 |
| 工具尺寸 | 无工具总数/总 schema 上限；MCP 单工具序列化 ≤ 8,000 B、描述 ≤ 1,000 B、工具名 ≤ 128 | `tools/src/responses_api.rs:14`、`tools/src/mcp_tool.rs:9`、`codex-mcp/src/tools.rs:226` | Cloud 128 个工具上限对 Codex 正常用法够用 |
| 独立图片接口 | `gpt-image-2`，edit 最多 5 张输入图，生成结果 ≤ 32 MiB；走 `/images/generations`、`/images/edits` | `ext/image-generation/src/tool.rs:65-69` | BPS 图片接口只接 `gpt-image-2`、edit 单图，与客户端 5 张不一致 |

