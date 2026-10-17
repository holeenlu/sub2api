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

此片段用于解释默认语义，不应覆盖生产其他设置。尝试预算为 0 时保留各路径原有重试边界；首输出时限为 0 时不启用新的首内容计时。启用尝试预算和首输出时限后，逻辑 turn 共用绝对期限，排队、调度、退避、响应头和首语义输出等待都纳入。收到实际内容后首输出计时停止，后续流使用既有读超时。

HTTP 原生流、HTTP 透传和 WS→HTTP bridge 都覆盖响应头及正文等待。网关在发送前耗尽预算不计作该账号的上游故障。命中托管工具或私有历史保护仍可能终止恢复；增加超时不会增加 OpenAI 容量。

## 诊断与上线验收

现有 Ops `retry_stopped` 事件记录实际上游模式/协议、created 到失败时间、下游提交状态、工具阻断类型、上下文可迁移性及可用的尝试/时间预算。WS 记录已写事件计数，HTTP 使用其现有语义输出状态，不伪造不可知计数。记录不包含提示词、密文、工具名称或参数。

先发布测试版本，通过加密续聊、动态工具、延迟错误、取消、凭据撤销、配置刷新、部分用量和失败后继续下一轮的验收，再做固定会话的 5%→20% 灰度。按同客户端/模型/推理强度/账号/代理比较完成率、P95 墙钟时间、额外上游用量与缓存；Ops 错误条数不能直接当失败率分母。

ctx_pool/bridge 对照、H1/H2 或代理出口调整属于后续生产试验。多 lane、steering、永久历史存储需独立需求及测试，不随本修复开放。发现丢上下文、重复副作用、跨用户归属或重复账单立即停止灰度。

## BPS HTTP/SSE 与客户端 WS（2026-10-12）

BPS 复用本契约的完整历史、取消归属、真实响应 ID、禁止盲目重放、共享尝试预算与计费规则。客户端 WS 在现有 ingress 会话所有者内转换为 BPS HTTP/SSE；BPS bridge 只适配事件，不另建 reader 或会话生命周期。请求失败保留最后成功锚点；工具声明只有省略时继承，显式空数组清空。首输出期限覆盖授权/附件/compact/上游响应头与元数据等待，keepalive 不续期，语义输出后转由无进展超时约束。详细范围与验收见 [BPS 适配决定](bps/upstream-changes.md)和[验收记录](bps/VALIDATION.md)。
