# OpenAI 客户端与上游协议

普通 OpenAI 请求默认跟随客户端的实际入站协议。账号开启 WS 能力不会把客户端 HTTP 请求升级为上游 WS；管理员仍可显式选择保留源方案的 `http_bridge` 模式。核心模式和转换流程一致，整体实现还包含本仓库的 BPS、预热、续聊与准入增强，不能视为上游代码的逐字副本。

| 客户端 | 实际生成通道 | 中转站连接上游 | 返回客户端 |
| --- | --- | --- | --- |
| HTTP Responses | 原生 OpenAI | HTTP | SSE 或非流式 JSON |
| WebSocket Responses | 原生 OpenAI | WebSocket | WebSocket 事件 |
| WebSocket Responses，手动选择 `http_bridge` | 原生 OpenAI | HTTP / SSE | 转换为 WebSocket 事件 |
| HTTP Responses | Excel / BPS | HTTP | SSE 或非流式 JSON |
| WebSocket Responses | Excel / BPS | HTTP / SSE | 转换为 WebSocket 事件 |

原生 WS 仍受全局开关、账号 WS 能力、权限和调度约束。不满足 WS 条件时返回明确错误，不自动改用普通 HTTP。绑定仅支持 HTTP 的 OAuth 传输插件的账号要求客户端使用 HTTP，或显式选择手动 HTTP 桥接，不能绕过插件改走原生 WS。Grok 等 HTTP 上游的独立兼容适配不属于普通 OpenAI 路由。

## 已移除的可选转换

- 账号编辑和批量编辑中的「HTTP 流式 WS 加速」已移除。历史 `extra.openai_oauth_ws_sse_acceleration` 即使为 true 也不再影响路由，账号新增、编辑和批量更新会忽略该字段。
- 普通 WS 首包不再因为大小超过阈值而改走 HTTP。旧 `gateway.openai_ws.http_bridge_enabled` 和 `http_bridge_threshold_bytes` 只用于这一自动策略，相关结构字段和默认值已删除；旧配置文件中的这两个键按未使用字段忽略，不影响启动或手动 `http_bridge` 模式。WS 帧大小限制仍独立生效。

上述兼容处理不需要数据库迁移。HTTP 请求始终进入 HTTP 转发分支；原生 WS 请求没有透明 HTTP 回退。`/admin/accounts` 的「WS mode → HTTP 桥接（http_bridge）」及其创建、编辑、批量设置保留，历史账号和默认模式中的 `http_bridge` 继续有效，不改为 `ctx_pool`。该手动模式仍受 `mode_router_v2_enabled` 开关控制。

## BPS 适配与模型切换

BPS 只支持 HTTP/SSE，因此保留客户端 WS 入站的协议适配、增量历史重建、工具调用结果、预热不计费、保活和部分用量记录。详见 [Excel / BPS](excel-bps.md)。

首次请求需要 BPS 不支持的托管能力且采用原生回退政策时，HTTP 客户端走原生 HTTP，WS 客户端默认走原生 WS，显式选择手动 `http_bridge` 则仍走 HTTP/SSE。决定协议时不修改账号的 BPS 配置，发送前仍校验最新账号状态。

`generate=false` 预热与正式请求使用同一套工具能力判断，避免预热选中 BPS、正式请求却要求切换到原生 WS 而反复重连。只有实际选中 BPS 或手动 HTTP 桥接的预热才在桥接器本地应答；原生 WS 预热保留 `generate=false` 发往上游。

一条连接已经使用原生 WS 后切换到 BPS，或已经使用 BPS 后切换到普通模型/原生托管能力，网关在新一轮发送前以 `1008 model switch requires reconnect` 结束连接。客户端重连后按首帧选择对应协议。显式选用手动 `http_bridge` 时，保留同一连接内经 HTTP/SSE 切换到原生模型/托管能力的原有行为。该路由变更不计为账号故障。

## 等待时间

移除可选转换消除了 HTTP 请求误入 WS 连接池的等待路径。上游生成速度、HTTP 超时和客户端重试仍各自生效；本次变更没有新增整段客户端会话的等待期限。

## 2026-10-04 源方案核对

本次使用 Git 源码和远端 SHA 核对，未同步、合并或部署上游变更：

- 官方 `Wei-Shaw/sub2api` 的 `main`：`b8dece9000c68815a5b867ca5a1e6f236e173905`。
- `ranxi2001/sub2api` 的 `production`：`0ae36e501952000c5c910a2e616c6e0861f66a49`。
- 手动模式来源为 zy6p 的 [模式及选择器提交](https://github.com/Wei-Shaw/sub2api/commit/901958ba1b8ee51238d687aa932e215fa5cb7f51)，实际入口分支由 [显式桥接修复](https://github.com/Wei-Shaw/sub2api/commit/906be3f742c99675ddcd54379e425adcb0cf1291) 补齐。它与后来 ranxi 的 HTTP→WS 加速是两个独立功能。

| 核对项 | 结论 |
| --- | --- |
| WS mode 的值、历史值处理、提示选择 | `frontend/src/utils/openaiWsMode.ts` 与两份源基线逐字一致。 |
| 账号 WS 协议解析器 | `openai_ws_protocol_resolver.go` 与两份源基线逐字一致。 |
| 模式字符串与默认模式规范化 | `normalizeOpenAIWSIngressMode`、`normalizeOpenAIWSIngressDefaultMode` 与两份源基线逐字一致。 |
| 创建、编辑、批量保存 | 保留同一 `http_bridge` 选项及 OAuth/API Key 专属 mode/enabled 字段，未转换为 `ctx_pool`。 |
| 生效条件 | 保留 `mode_router_v2_enabled` 门控。显式 `http_bridge` 强制进入 HTTP 分支，不依赖已移除的大小阈值。 |
| 核心转换 | 保留 WS 入站 → HTTP Responses 请求 → SSE 事件转 WS 帧；请求准备函数 `prepareOpenAIWSHTTPBridgeBody` 与两份源基线逐字一致。 |
| 错误事件格式 | `buildOpenAIWSHTTPBridgeErrorEvent`、`buildOpenAIWSHTTPBridgeFailedEvent` 与两份源基线逐字一致。 |
| 本地增强 | BPS 上游适配、桥接预热本地应答且不计费、完整输出历史重建、原生/工具续聊状态、发送前最新准入和 RPM 校验。这些代码并非上游逐字版本，继续保留。 |
| 按新原则的差异 | 移除 HTTP→WS 加速及按大小自动桥接；未显式选择桥接的 HTTP 传输插件使用原生 WS 时明确拒绝，避免隐式换协议。手动桥接与 BPS 适配保留。 |

审查修正了预热误选 BPS 的边界，并把 BPS 判定统一为直接读取当前 `tool_choice` 与本连接继承的 `tools`，不再每轮构造临时 JSON。普通非 BPS 账号直接跳过这部分处理；账号副本只用于旧版全模型 BPS 的协议选择，原账号仍负责准入和连接绑定。

验证：服务层相关回归 890 项、处理层相关回归 112 项通过（均包含子用例）；配置与 BPS 包测试、关键路径 race 检查及服务端构建通过。覆盖了手动桥接、BPS/原生切换、预热、继承工具权限、旧配置加载和超过旧 15 MiB 阈值的原生 WS 请求。测试使用模拟上游，不代表生产延迟测量；本次没有推送或部署。
