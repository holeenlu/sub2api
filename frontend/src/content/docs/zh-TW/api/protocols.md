## 按 API Key 分組選擇協議

模型與權限由實際 API Key 分組動態決定。用同一把 Key 發現模型並向目標端點發送最小請求；全域模型目錄和 `GET /v1/models` 列出的候選都不證明目前有可排程帳號，更不證明所有參數均可用。

| 協議 | 入口 | 閘道器路由邊界 |
| --- | --- | --- |
| OpenAI Chat | `POST /v1/chat/completions` | OpenAI、Grok、Kimi、智譜、DeepSeek、MiniMax、OpenCodeGo 進入 OpenAI 相容處理鏈；其他分組可能進入協議轉換鏈 |
| OpenAI Responses | `POST /v1/responses` | 平台分流同上；`POST /v1/responses/*subpath` 僅允許安全的上游路徑，不代表完整 CRUD |
| Anthropic Messages | `POST /v1/messages` | Anthropic 類分組走 Messages 鏈；上述 OpenAI 相容分組可橋接，內容塊與 usage 未必逐欄位等價 |
| Gemini 原生 REST | `GET /v1beta/models`、`POST /v1beta/models/{model}:{action}` | Gemini 分組；action 僅支援 `generateContent`、`streamGenerateContent`、`countTokens` |
| 同步與非同步 Images | `/v1/images/generations`、`/edits`、追加 `/async`、`GET /v1/images/tasks/{task_id}` | 僅 OpenAI、Grok 分組；需開啟影像權限和匹配帳號能力；非同步還需物件儲存 |
| Embeddings | `POST /v1/embeddings` | 僅 OpenAI 平台分組；其他平台返回 404 |
| Grok 媒體/搜尋/語音 | `/v1/videos...`、`/v1/web_search`、`/v1/x_search`、`/v1/tts` 等 | 多數僅 Grok；少數影片建立/查詢允許解析到 Grok 的複合分組 |
| 模型發現 | `GET /v1/models`、`GET /v1/models/{model}` | 目前 Key 候選；`?client_version=...` 是不同的 Codex manifest，不是通用列表 |
| Token count | `/v1/messages/count_tokens`、`/v1/responses/input_tokens`、Gemini `:countTokens` | 可能轉發，也可能由本地 tokenizer 估算，不等於最終生成 usage |

## 鑑權和呼叫約束

OpenAI 風格使用 `Authorization: Bearer $TAPMODELS_API_KEY`；Anthropic SDK 使用 `x-api-key` 和 `anthropic-version`；Gemini SDK 使用 `x-goog-api-key`。Gemini 鑑權也接受 Bearer、`x-api-key`、URL 查詢參數 `key`，但 URL Key 容易進入代理日誌。閘道器按端點執行部署設定的請求體上限、分組模型白名單，以及適用的額度、並行和內容策略。模型的工具、影像輸入、結構化輸出、快取和內建工具還受具體帳號與上游實現限制。

主要 OpenAI/Anthropic `/v1` 介面註冊了若干無 `/v1` 別名供用戶端相容，新整合建議使用 `/v1`。`/backend-api/codex/*` 和 `/antigravity/*` 屬於專用用戶端/平台路徑。`GET /v1/responses` 是 WebSocket Upgrade 入口，不是 Retrieve Response；未升級的普通 GET 返回 426。

## 回應與錯誤

OpenAI 風格錯誤通常為 `{"error":{"type":"...","message":"..."}}`，Anthropic 為 `{"type":"error","error":{...}}`，Gemini 為 `{"error":{"code":400,"message":"...","status":"INVALID_ARGUMENT"}}`。SSE 需等待對應協議的完成事件；流中錯誤或完成前斷開應標記未知/截斷。重試生成請求可能重複輸出和工具副作用，應使用應用自己的冪等機制。

以上是原始碼能力而非動態分組可用性的生產保證。平台路由見 `backend/internal/server/routes/gateway.go`；實際處理鏈見 `backend/internal/handler/gateway_handler*.go`、`openai_gateway_handler.go`、`gemini_v1beta_handler.go`、`grok_media.go`、`grok_audio.go`。
