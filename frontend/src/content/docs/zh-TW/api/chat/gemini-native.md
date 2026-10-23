## 支援的原生端點

```http
GET  /v1beta/models
GET  /v1beta/models/{model}
POST /v1beta/models/{model}:generateContent
POST /v1beta/models/{model}:streamGenerateContent?alt=sse
POST /v1beta/models/{model}:countTokens
x-goog-api-key: $TAPMODELS_API_KEY
```

普通 `/v1beta` 僅接受 Gemini 分組。另有 `/antigravity/v1beta` 專用字首，它強制選擇 Antigravity 平台，不能用它推斷普通 Gemini 分組的帳號或模型能力。

處理鏈只允許 `generateContent`、`streamGenerateContent`、`countTokens`。`embedContent`、批次 embedding、快取內容、檔案和微調未在這組閘道器路由實現；即使萬用字元 URL 能匹配，action 也會被拒絕。

## 模型發現

```bash
curl "$TAPMODELS_BASE_URL/v1beta/models" -H "x-goog-api-key: $TAPMODELS_API_KEY"
```

回應使用 Gemini `models[]` 信封，`name` 通常為 `models/YOUR_MODEL_ID`。列表可能來自上游，也可能在特定帳號無法直接發現時回退到程式碼預設項，並經過分組白名單過濾。可見性不代表即時可排程。

## generateContent 請求

```bash
curl "$TAPMODELS_BASE_URL/v1beta/models/YOUR_MODEL_ID:generateContent" \
  -H "x-goog-api-key: $TAPMODELS_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "systemInstruction":{"parts":[{"text":"Answer briefly."}]},
    "contents":[{"role":"user","parts":[{"text":"Explain idempotency."}]}],
    "generationConfig":{"temperature":0.2,"maxOutputTokens":256}
  }'
```

| 欄位 | 必需 | 說明 |
| --- | --- | --- |
| URL `{model}` | 是 | 目前 Key 開放的模型 ID；路徑片段只允許字母、數字、`_`、`-`、`.` |
| `contents` | 是 | 對話陣列；每項含 `role` 和非空 `parts` |
| `parts[].text` | 視任務 | 文字；多模態可用 `inlineData`、`fileData`，需模型與帳號支援 |
| `systemInstruction` | 否 | Gemini 頂層系統指令 |
| `generationConfig` | 否 | 溫度、輸出上限、停止序列、回應 MIME/Schema 等，以目標模型為準 |
| `tools` / `toolConfig` | 否 | 函式宣告與選擇；結果用 `functionResponse` part 回傳 |
| `safetySettings` | 否 | 可接受範圍依帳號類型和上游模型 |

閘道器會過濾 `parts` 為空的訊息，並可能為嚴格上游補函式呼叫 thought signature；用戶端仍應傳送規範請求。成功回應需遍歷 `candidates[]` 與 `content.parts`，並讀取 `finishReason`、`usageMetadata` 和可能的 `promptFeedback`。候選為空或非正常 finish reason 不能視為完整結果。

## SSE 流

```bash
curl -N "$TAPMODELS_BASE_URL/v1beta/models/YOUR_MODEL_ID:streamGenerateContent?alt=sse" \
  -H "x-goog-api-key: $TAPMODELS_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"role":"user","parts":[{"text":"Explain idempotency."}]}]}'
```

串流由 URL action 決定，不使用請求體 `stream`。每個 `data:` 是 Gemini 回應片段；按候選 index 和 part 累積，在最終片段讀取 finish reason 與 usage。完整終止前斷開代表可能截斷。部分 OAuth/Code Assist 帳號在非流入口內部也會走上游流再聚合，用戶端不能由下游格式推斷上游傳輸。

## 計數、錯誤與限制

`:countTokens` 使用 `contents` 形狀並返回 `totalTokens`。Antigravity OAuth 帳號鏈目前固定返回預留位置 `0`，不能當成真實計數或用於容量規劃；普通 Gemini 分組若選擇到此類帳號也受此限制。錯誤是 Google 風格：`{"error":{"code":400,"message":"...","status":"INVALID_ARGUMENT"}}`。常見原因包括 Key/分組不匹配、模型路徑無效、action 不支援、空請求體、白名單、額度/並行和上游失敗。

實現見 `backend/internal/server/routes/gateway.go`、`handler/gemini_v1beta_handler.go`、`service/gemini_messages_compat_service.go`、`service/antigravity_gateway_gemini.go`、`service/vertex_service_account.go`。
