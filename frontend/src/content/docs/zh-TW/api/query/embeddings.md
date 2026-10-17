## 端點與平台

```http
POST /v1/embeddings
Authorization: Bearer $TAPMODELS_API_KEY
Content-Type: application/json
```

閘道器路由只允許 OpenAI 平台分組；其他分組返回 404 `not_found_error` 和 `Embeddings API is not supported for this platform`。還需分組模型開放、實際帳號具有 embeddings 能力和可用額度。`GET /v1/models` 可見某模型不保證它是 embedding 模型。

## 請求與回應

```bash
curl "$TAPMODELS_BASE_URL/v1/embeddings" \
  -H "Authorization: Bearer $TAPMODELS_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_EMBEDDING_MODEL_ID","input":"Idempotency means repeatable effects."}'
```

`model` 是必填非空字串。`input` 按目標 OpenAI-compatible embedding 模型接受文字或陣列；`encoding_format`、`dimensions` 等額外欄位由上游目標驗證，閘道器不會保證所有帳號均支援。用戶端應先以單段文字和精確模型 ID 驗證。

典型回應（示意，非固定向量維度）：

```json
{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.012,-0.008]}],"model":"YOUR_EMBEDDING_MODEL_ID","usage":{"prompt_tokens":8,"total_tokens":8}}
```

按 `index` 對齊批次輸入，驗證向量維度和是否包含非有限值，不把 2 維範例當成實際維度。用戶端不得按 Chat SSE 處理此同步 JSON 端點。

## 錯誤與實現

空體、非 JSON 或缺失 `model` 返回 400 `invalid_request_error`；Key 無效返回 401；平台不符返回 404；沒有 embeddings 能力帳號或上游不可用可能返回排程/上游錯誤。閘道器執行模型對應、內容策略、並行和額度資格檢查，隨後選擇具有 `Embeddings` endpoint capability 的帳號。

平台限制見 `backend/internal/server/routes/gateway.go`；模型和帳號選擇見 `backend/internal/handler/openai_embeddings.go`；實際轉發見 `backend/internal/service/openai_embeddings.go`。本文未驗證生產分組與上游回傳向量。
