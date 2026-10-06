## 支持的原生端点

```http
GET  /v1beta/models
GET  /v1beta/models/{model}
POST /v1beta/models/{model}:generateContent
POST /v1beta/models/{model}:streamGenerateContent?alt=sse
POST /v1beta/models/{model}:countTokens
x-goog-api-key: $KDAN_API_KEY
```

普通 `/v1beta` 仅接受 Gemini 分组。另有 `/antigravity/v1beta` 专用前缀，它强制选择 Antigravity 平台，不能用它推断普通 Gemini 分组的账号或模型能力。

处理链只允许 `generateContent`、`streamGenerateContent`、`countTokens`。`embedContent`、批量 embedding、缓存内容、文件和微调未在这组网关路由实现；即使通配 URL 能匹配，action 也会被拒绝。

## 模型发现

```bash
curl "$KDAN_BASE_URL/v1beta/models" -H "x-goog-api-key: $KDAN_API_KEY"
```

响应使用 Gemini `models[]` 信封，`name` 通常为 `models/YOUR_MODEL_ID`。列表可能来自上游，也可能在特定账号无法直接发现时回退到代码默认项，并经过分组白名单过滤。可见性不代表即时可调度。

## generateContent 请求

```bash
curl "$KDAN_BASE_URL/v1beta/models/YOUR_MODEL_ID:generateContent" \
  -H "x-goog-api-key: $KDAN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "systemInstruction":{"parts":[{"text":"Answer briefly."}]},
    "contents":[{"role":"user","parts":[{"text":"Explain idempotency."}]}],
    "generationConfig":{"temperature":0.2,"maxOutputTokens":256}
  }'
```

| 字段 | 必需 | 说明 |
| --- | --- | --- |
| URL `{model}` | 是 | 当前 Key 开放的模型 ID；路径片段只允许字母、数字、`_`、`-`、`.` |
| `contents` | 是 | 对话数组；每项含 `role` 和非空 `parts` |
| `parts[].text` | 视任务 | 文本；多模态可用 `inlineData`、`fileData`，需模型与账号支持 |
| `systemInstruction` | 否 | Gemini 顶层系统指令 |
| `generationConfig` | 否 | 温度、输出上限、停止序列、响应 MIME/Schema 等，以目标模型为准 |
| `tools` / `toolConfig` | 否 | 函数声明与选择；结果用 `functionResponse` part 回传 |
| `safetySettings` | 否 | 可接受范围依账号类型和上游模型 |

网关会过滤 `parts` 为空的消息，并可能为严格上游补函数调用 thought signature；客户端仍应发送规范请求。成功响应需遍历 `candidates[]` 与 `content.parts`，并读取 `finishReason`、`usageMetadata` 和可能的 `promptFeedback`。候选为空或非正常 finish reason 不能视为完整结果。

## SSE 流

```bash
curl -N "$KDAN_BASE_URL/v1beta/models/YOUR_MODEL_ID:streamGenerateContent?alt=sse" \
  -H "x-goog-api-key: $KDAN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"role":"user","parts":[{"text":"Explain idempotency."}]}]}'
```

流式由 URL action 决定，不使用请求体 `stream`。每个 `data:` 是 Gemini 响应片段；按候选 index 和 part 累积，在最终片段读取 finish reason 与 usage。完整终止前断开代表可能截断。部分 OAuth/Code Assist 账号在非流入口内部也会走上游流再聚合，客户端不能由下游格式推断上游传输。

## 计数、错误与限制

`:countTokens` 使用 `contents` 形状并返回 `totalTokens`。Antigravity OAuth 账号链当前固定返回占位 `0`，不能当成真实计数或用于容量规划；普通 Gemini 分组若选择到此类账号也受此限制。错误是 Google 风格：`{"error":{"code":400,"message":"...","status":"INVALID_ARGUMENT"}}`。常见原因包括 Key/分组不匹配、模型路径无效、action 不支持、空请求体、白名单、额度/并发和上游失败。

实现见 `backend/internal/server/routes/gateway.go`、`handler/gemini_v1beta_handler.go`、`service/gemini_messages_compat_service.go`、`service/antigravity_gateway_gemini.go`、`service/vertex_service_account.go`。
