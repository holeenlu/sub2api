## 端点与平台

```http
POST /v1/embeddings
Authorization: Bearer $TOKENSAVY_API_KEY
Content-Type: application/json
```

网关路由只允许 OpenAI 平台分组；其他分组返回 404 `not_found_error` 和 `Embeddings API is not supported for this platform`。还需分组模型开放、实际账号具有 embeddings 能力和可用额度。`GET /v1/models` 可见某模型不保证它是 embedding 模型。

## 请求与响应

```bash
curl "$TOKENSAVY_BASE_URL/v1/embeddings" \
  -H "Authorization: Bearer $TOKENSAVY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_EMBEDDING_MODEL_ID","input":"Idempotency means repeatable effects."}'
```

`model` 是必填非空字符串。`input` 按目标 OpenAI-compatible embedding 模型接受文本或数组；`encoding_format`、`dimensions` 等额外字段由上游目标验证，网关不会保证所有账号均支持。客户端应先以单段文本和精确模型 ID 验证。

典型响应（示意，非固定向量维度）：

```json
{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.012,-0.008]}],"model":"YOUR_EMBEDDING_MODEL_ID","usage":{"prompt_tokens":8,"total_tokens":8}}
```

按 `index` 对齐批量输入，验证向量维度和是否包含非有限值，不把 2 维示例当成实际维度。客户端不得按 Chat SSE 处理此同步 JSON 端点。

## 错误与实现

空体、非 JSON 或缺失 `model` 返回 400 `invalid_request_error`；Key 无效返回 401；平台不符返回 404；没有 embeddings 能力账号或上游不可用可能返回调度/上游错误。网关执行模型映射、内容策略、并发和额度资格检查，随后选择具有 `Embeddings` endpoint capability 的账号。

平台限制见 `backend/internal/server/routes/gateway.go`；模型和账号选择见 `backend/internal/handler/openai_embeddings.go`；实际转发见 `backend/internal/service/openai_embeddings.go`。本文未验证生产分组与上游返回向量。
