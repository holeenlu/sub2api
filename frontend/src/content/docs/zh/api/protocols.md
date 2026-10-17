## 按 API Key 分组选择协议

模型与权限由实际 API Key 分组动态决定。用同一把 Key 发现模型并向目标端点发送最小请求；全局模型目录和 `GET /v1/models` 列出的候选都不证明当前有可调度账号，更不证明所有参数均可用。

| 协议 | 入口 | 网关路由边界 |
| --- | --- | --- |
| OpenAI Chat | `POST /v1/chat/completions` | OpenAI、Grok、Kimi、智谱、DeepSeek、MiniMax、OpenCodeGo 进入 OpenAI 兼容处理链；其他分组可能进入协议转换链 |
| OpenAI Responses | `POST /v1/responses` | 平台分流同上；`POST /v1/responses/*subpath` 仅允许安全的上游路径，不代表全量 CRUD |
| Anthropic Messages | `POST /v1/messages` | Anthropic 类分组走 Messages 链；上述 OpenAI 兼容分组可桥接，内容块与 usage 未必逐字段等价 |
| Gemini 原生 REST | `GET /v1beta/models`、`POST /v1beta/models/{model}:{action}` | Gemini 分组；action 仅支持 `generateContent`、`streamGenerateContent`、`countTokens` |
| 同步与异步 Images | `/v1/images/generations`、`/edits`、追加 `/async`、`GET /v1/images/tasks/{task_id}` | 仅 OpenAI、Grok 分组；需开启图像权限和匹配账号能力；异步还需对象存储 |
| Embeddings | `POST /v1/embeddings` | 仅 OpenAI 平台分组；其他平台返回 404 |
| Grok 媒体/搜索/语音 | `/v1/videos...`、`/v1/web_search`、`/v1/x_search`、`/v1/tts` 等 | 多数仅 Grok；少数视频创建/查询允许解析到 Grok 的复合分组 |
| 模型发现 | `GET /v1/models`、`GET /v1/models/{model}` | 当前 Key 候选；`?client_version=...` 是不同的 Codex manifest，不是通用列表 |
| Token count | `/v1/messages/count_tokens`、`/v1/responses/input_tokens`、Gemini `:countTokens` | 可能转发，也可能由本地 tokenizer 估算，不等于最终生成 usage |

## 鉴权和调用约束

OpenAI 风格使用 `Authorization: Bearer $TAPMODELS_API_KEY`；Anthropic SDK 使用 `x-api-key` 和 `anthropic-version`；Gemini SDK 使用 `x-goog-api-key`。Gemini 鉴权也接受 Bearer、`x-api-key`、URL 查询参数 `key`，但 URL Key 容易进入代理日志。网关按端点执行部署配置的请求体上限、分组模型白名单，以及适用的额度、并发和内容策略。模型的工具、图像输入、结构化输出、缓存和内置工具还受具体账号与上游实现限制。

主要 OpenAI/Anthropic `/v1` 接口注册了若干无 `/v1` 别名供客户端兼容，新集成建议使用 `/v1`。`/backend-api/codex/*` 和 `/antigravity/*` 属于专用客户端/平台路径。`GET /v1/responses` 是 WebSocket Upgrade 入口，不是 Retrieve Response；未升级的普通 GET 返回 426。

## 响应与错误

OpenAI 风格错误通常为 `{"error":{"type":"...","message":"..."}}`，Anthropic 为 `{"type":"error","error":{...}}`，Gemini 为 `{"error":{"code":400,"message":"...","status":"INVALID_ARGUMENT"}}`。SSE 需等待对应协议的完成事件；流中错误或完成前断开应标记未知/截断。重试生成请求可能重复输出和工具副作用，应使用应用自己的幂等机制。

以上是源码能力而非动态分组可用性的生产保证。平台路由见 `backend/internal/server/routes/gateway.go`；实际处理链见 `backend/internal/handler/gateway_handler*.go`、`openai_gateway_handler.go`、`gemini_v1beta_handler.go`、`grok_media.go`、`grok_audio.go`。
