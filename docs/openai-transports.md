# OpenAI 客户端与上游协议

普通 OpenAI 请求默认跟随客户端的实际入站协议。账号开启 WS 能力不会把客户端 HTTP 请求升级为上游 WS；管理员可以显式选择 `http_bridge` 模式。

| 客户端 | 实际生成通道 | 中转站连接上游 | 返回客户端 |
| --- | --- | --- | --- |
| HTTP Responses | 原生 OpenAI | HTTP | SSE 或非流式 JSON |
| WebSocket Responses | 原生 OpenAI | WebSocket | WebSocket 事件 |
| WebSocket Responses，手动选择 `http_bridge` | 原生 OpenAI | HTTP / SSE | 转换为 WebSocket 事件 |

原生 WS 仍受全局开关、账号 WS 能力、权限和调度约束。不满足 WS 条件时返回明确错误，不自动改用普通 HTTP。绑定仅支持 HTTP 的 OAuth 传输插件的账号要求客户端使用 HTTP，或显式选择手动 HTTP 桥接；不能绕过插件改走原生 WS。Grok 等 HTTP 上游的兼容适配继续保留。

## 手动桥接与续聊

账号创建、编辑、批量设置以及历史配置中的 `http_bridge` 模式继续有效。它仍受 `mode_router_v2_enabled` 开关控制，不改为 `ctx_pool`。客户端 WS 请求经 HTTP Responses 转发，SSE 事件转换成 WS 帧；工具续聊、previous_response_id 历史重建、发送前准入和 RPM 校验保持。

手动桥接的 `generate=false` 预热在桥接器本地应答；原生 WS 的预热保留 `generate=false` 并发往上游。取消和连接结束仍执行已有的用量收尾，避免把正常客户端退出归为账号故障。

## 已移除的转换

- 2026-10-04：移除账号“HTTP 流式 WS 加速”和按 WS 首包大小自动改走 HTTP 的策略。旧 `openai_oauth_ws_sse_acceleration` 不再影响路由；旧 `http_bridge_enabled` / `http_bridge_threshold_bytes` 配置按未使用字段忽略。
- 2026-10-05：移除 Excel/BPS 和旧独立平台的全部运行路径，参见 [升级说明](BPS_REMOVAL.md)。保留 Codex 降智检测、打票、原生工具与图片能力。

通用 WS 帧大小限制、`client_first_message_timeout_seconds`、上游读写超时和客户端重试各自生效。移除旧转换不会回退首包超时设置，也不改变模型的实际生成速度。

## 来源边界

手动模式沿用官方方案的 WS mode、选择器和显式桥接逻辑。2026-10-04 的源码对照基准为官方 `b8dece9000c68815a5b867ca5a1e6f236e173905` 与 ranxi `0ae36e501952000c5c910a2e616c6e0861f66a49`；来源与历史比较可从对应 Git 提交查阅。本仓库还保留自己的预热、续聊、准入、工具状态和用量收尾，不能把整个实现描述为上游逐字副本。
