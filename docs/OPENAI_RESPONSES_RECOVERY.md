# OpenAI Responses 恢复合同

适用于 HTTP Responses、Chat/Messages 兼容入口和客户端 WebSocket；共享于 KDAN、TapModels、Tokensavy。生产是否生效以镜像 revision 为准，Git 推送不会发布镜像或更新容器。

## 上下文与重放

- 密文 reasoning、compaction 和原有历史必须完整保留。`invalid_encrypted_content` 及已识别的加密签名校验失败不通过删密文、删推理项或盲删 `previous_response_id` 修复。
- 同一凭据归属的账号可重试完整的加密历史；跨账号恢复还要求请求无私有文件、向量库、容器、密文及未展开的服务端上下文引用。
- HTTP 请求中的 `previous_response_id` 可以由上游解析。网关只有掌握完整且匹配的历史时才能把它替换成完整输入；工具调用配对不等于完整历史。
- 不确定执行结果的重试同时检查顶层工具、namespace 子工具、`additional_tools`、`tool_search_output.tools`。客户端工具发现和只读搜索不自动阻断；MCP、计算机操作、代码执行等需要执行安全证明。未知工具、重复语义字段、无法展开的历史不猜测为安全。
- 同一 turn 的同账号重试、账号切换和内部请求修复共用已启用的尝试预算。原始失败 attempt 的 response ID 不进入成功用量。

## WebSocket 生命周期

- 每个客户端连接保持一个 reader，写入串行化。当前仍是单 lane；合法 `stream_id` 在 bridge 转 HTTP 时移除，在返回 WS 事件时补回。`event_id` 只用于对应请求错误的关联，不能把 create 的 ID 当作 cancel 的 ID。
- 已交付本轮请求错误后，bridge 保留客户端 socket 与此前已确认的历史。没有上游 response ID 时返回请求错误，不捏造一个可用于续聊或取消的 ID。失败尝试不成为已确认的重放历史。
- 原生 WS 保留现有有界身份暂存和取消归属校验；不通过无限延长暂存规避已提交输出后的恢复限制。已提交语义输出不能透明重放。
- 后续 turn 重新核对 API Key、账号资格、分组和模型。凭据、代理、端点或相关模式配置变化时重建上游；可证明完整且归属一致时重放。凭据归属变化且历史不可迁移时要求重连并提供完整上下文。
- 每轮用量任务冻结账号、用户、模型及请求摘要。异常或断连时仅记录实际观察到的用量，继续使用原账单去重；未知用量不估算。

## 时限与配置

继续使用现有配置，无新增开关：

```yaml
gateway:
  openai_ws:
    mode_router_v2_enabled: true
  openai_turn_max_attempts: 0
  openai_first_output_timeout_seconds: 0
  openai_high_effort_first_output_timeout_seconds: 0
```

此片段用于解释当前默认语义，不应覆盖生产其他设置。尝试预算为 0 时使用最多 8 次生成尝试，正值可降低上限；准备阶段重试另有有界额度。标准首输出时限为 0 时使用 300 秒，并非关闭计时；high/xhigh/max 的专用值为 0 时，继承显式标准时限，若两项均为 0 则使用 600 秒。逻辑 turn 共用绝对期限，排队、调度、退避、响应头和首语义输出等待都纳入；换号或换协议不重新开始计时。收到实际内容后首输出计时停止，后续流使用既有读超时。

2026-10-19 BPS 超时归因补齐：只收到 created/in_progress 等元数据不能解除首输出期限；期限耗尽按 first_output_timeout / 504 结束，并通过已有 retry_stopped / turn_budget 记录停止原因。已经建立流时 wire HTTP 状态仍可为 200，Ops 的逻辑失败状态与实际 BPS HTTP 状态分开保存。此次补齐不增加重试次数、不重置期限、不修改生产配置，也不证明上游无输出的具体原因。

HTTP 原生流、HTTP 透传和 WS→HTTP bridge 都覆盖响应头及正文等待。网关在发送前耗尽预算不计作该账号的上游故障。命中托管工具或私有历史保护仍可能终止恢复；增加超时不会增加 OpenAI 容量。

## 诊断与上线验收

HTTP 入站转上游 WS 时，对端明确发回关闭码 `1009 / StatusMessageTooBig` 表示其拒绝接收过大的消息。尚未向客户端提交内容时返回 `413 invalid_request_error / request_too_large`；仅心跳已发出时保留 HTTP 200，通过 `response.failed` 表达同一错误。已交付内容后不追加第二种错误响应，不自动截断历史、提高本地读上限或重发。

该关闭帧不是上游 HTTP 413：Ops 保留空的上游 HTTP 状态，并记录 `message_too_big`、WS 关闭码及可用的 `payload_bytes`（出站 JSON 序列化后的字节数，不含压缩/帧开销；未采集时不猜测）。本地 `read limited at ... bytes` / `ErrMessageTooBig` 不能误报为客户端请求过大。排查时按请求 ID 关联 `read_fail` 的字节数、事件数、账号和连接；对端上限不能从本地 16 MiB 读上限推断。

现有 Ops `retry_stopped` 事件记录实际上游模式/协议、created 到失败时间、下游提交状态、工具阻断类型、上下文可迁移性及可用的尝试/时间预算。WS 记录已写事件计数，HTTP 使用其现有语义输出状态，不伪造不可知计数。记录不包含提示词、密文、工具名称或参数。

先发布测试版本，通过加密续聊、动态工具、延迟错误、取消、凭据撤销、配置刷新、部分用量和失败后继续下一轮的验收，再做固定会话的 5%→20% 灰度。按同客户端/模型/推理强度/账号/代理比较完成率、P95 墙钟时间、额外上游用量与缓存；Ops 错误条数不能直接当失败率分母。

ctx_pool/bridge 对照、H1/H2 或代理出口调整属于后续生产试验。多 lane、steering、永久历史存储需独立需求及测试，不随本修复开放。发现丢上下文、重复副作用、跨用户归属或重复账单立即停止灰度。

## BPS HTTP/SSE 与客户端 WS（2026-10-12）

BPS 复用本契约的完整历史、取消归属、真实响应 ID、禁止盲目重放、共享尝试预算与计费规则。客户端 WS 在现有 ingress 会话所有者内转换为 BPS HTTP/SSE；BPS bridge 只适配事件，不另建 reader 或会话生命周期。请求失败保留最后成功锚点；工具声明只有省略时继承，显式空数组清空。首输出期限覆盖授权/附件/compact/上游响应头与元数据等待，keepalive 不续期，语义输出后转由无进展超时约束。详细范围与验收见 [BPS 适配决定](bps/upstream-changes.md)和[验收记录](bps/VALIDATION.md)。

## Lite 搜索历史与托管工具拒绝（2026-10-19）

`Hosted tool 'web_search' requires authorization and metering that are not supported by rustponsesapi` 不应只归为不可修复的上游限制。必须先排除网关是否给原本合法的 Lite 请求补入了托管工具：旧的 OAuth 搜索历史兼容器发现 `web_search_call` 后，会在 Lite 的 `input.additional_tools` 中追加 `web_search`，这与客户端执行工具的协议契约冲突。

- Lite 请求保留原搜索历史、推理密文、call_id、previous_response_id、客户端工具和 tool_choice；历史兼容器不注入托管搜索。`additional_tools` 不是把 hosted 工具改成客户端工具的转换器。
- 普通 Responses 仍保留原搜索历史修复：必要时补入缓存搜索声明；只有调用方未提供工具时才固定 tool_choice=none，避免额外搜索。已声明的 web.run 或托管搜索不重复注入。
- 现有 GPT-5.5 HTTP 兼容路径实际移除 Lite 标记后，在最终出站请求上应用普通协议的历史修复，并更新 Body、ContentLength、GetBody。入站请求保持不变，后续换到原生 Lite 账号时不继承这次标准协议声明。
- 此修复不删除用户明确声明的托管搜索，不扩大 400 重试、不改变模型、不新建网关或工具执行器。显式工具确不被目标支持时仍保留原错误；不能把本地修复描述成已为所有上游新增搜索能力。

依据是 Codex 客户端已有的 Lite 行为：`core/src/tools/spec_plan.rs::hosted_model_tool_specs` 在 Lite 下返回空托管工具集，`core/src/client.rs::build_responses_request` 使用 additional_tools 携带客户端工具。源提交 `ffe90cb5c37d4cf6ccfa9de0126a5ff61cb959e5`（Use standalone tools for Responses Lite），本次对照的 Codex HEAD 为 `89461ea6833832b0551ce24ee32205824d5c108d`。

回归覆盖 HTTP 普通/透传、ctx_pool、WS passthrough、HTTP bridge、同连接后续轮，以及 GPT-5.5 非 Lite 兼容。旧代码在受控上游返回相同 hosted-tool 拒绝；补丁后同一输入保留历史并完成两轮。受控测试证明修复该本地注入缺陷，不替代生产真实搜索和错误率的上线验收。无需新增配置；发布并部署后才影响线上请求。


## 已观测用量与断连收尾（2026-10-19 工作区复核）

HTTP JSON/SSE、Chat/Messages 兼容以及 WS ctx_pool/bridge/passthrough 在失败或断开时保留实际观测用量，继续交给既有账单幂等入口，不因异常丢弃结果，也不将未知用量估算成消耗。已计量尝试停止透明重放，保留原错误状态；失败 response ID 不作为成功续聊锚点。

HTTP detached 请求在客户端断开后沿用 1200 ms 的既有 usage-drain 上限，涵盖等待响应头和读取正文，上游心跳不能延长该窗口。非流式请求接收 SSE 时按有效终态结束读取，无终态/无效 JSON 不能成为成功响应。恢复、计费和语义输出边界不因诊断增强而放宽。复核、清理和验证范围见 [工作区复核报告](WORKSPACE_REVIEW_2026-10-19.md)。
