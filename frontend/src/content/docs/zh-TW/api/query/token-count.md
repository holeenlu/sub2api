## 三種 Token Count 入口

| 協議 | 請求 | 成功欄位 | 語義 |
| --- | --- | --- | --- |
| Anthropic | `POST /v1/messages/count_tokens` | `input_tokens` | Anthropic 鏈可轉發；OpenAI 鏈可橋接；Grok 和部分相容提供商為本地估算 |
| Responses | `POST /v1/responses/input_tokens` | `object: response.input_tokens`、`input_tokens` | 官方相容帳號可轉發；不支援此入口及部分相容帳號會回退本地估算 |
| Gemini | `POST /v1beta/models/{model}:countTokens` | `totalTokens` | 僅 Gemini 分組；普通 Gemini 帳號按實現返回，Antigravity OAuth 路徑目前固定返回預留位置 `0` |

計數不是最終帳單預測。協議包裝、工具 Schema、影像、快取、推理 token、上游 tokenizer 和模型對應都可能使結果與生成回應的 usage 不同。

## Anthropic Messages 計數

```bash
curl "$KDAN_BASE_URL/v1/messages/count_tokens" \
  -H "x-api-key: $KDAN_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","system":"Answer briefly.","messages":[{"role":"user","content":"What is idempotency?"}]}'
```

```json
{"input_tokens":19}
```

請求體沿用 Messages 輸入，可包括 `system`、`messages`、`tools` 和 thinking 欄位。Grok 路徑不選帳號、不存取上游，經過協議轉換和本地 tokenizer 估算，只適合容量規劃。Anthropic 相容上游明確沒有計數介面時可能返回 404。

## Responses input_tokens

```bash
curl "$KDAN_BASE_URL/v1/responses/input_tokens" \
  -H "Authorization: Bearer $KDAN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","instructions":"Answer briefly.","input":"What is idempotency?"}'
```

```json
{"object":"response.input_tokens","input_tokens":19}
```

`model` 是必填非空字串，其餘欄位沿用 Responses 輸入，包括字串或類型化 `input`、`instructions` 和工具定義。本地回退同樣返回 HTTP 200 和同一信封，用戶端不能僅靠格式判斷是否為上游精確計數；最終應讀取實際生成 usage。

## Gemini countTokens

```bash
curl "$KDAN_BASE_URL/v1beta/models/YOUR_MODEL_ID:countTokens" \
  -H "x-goog-api-key: $KDAN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"role":"user","parts":[{"text":"What is idempotency?"}]}]}'
```

```json
{"totalTokens":19}
```

上面的數值僅為回應形狀範例。普通 `/v1beta` 僅 Gemini 分組可用；如果該分組選擇到 Antigravity OAuth 帳號，目前實現恆返回 `{"totalTokens":0}`，不能用於容量規劃或精確計費估算。使用同一 Key 查詢 `/v1beta/models`，並移除返回 `name` 中的 `models/` 字首後放入 URL。

## 錯誤與實現邊界

缺少 Key、空請求體、無 `model`、模型未開放、無可排程帳號按對應協議回傳錯誤。計數不寫入生成 usage 帳單，但仍經過驗證、分組、白名單、內容審核、額度資格和請求體上限。實現見 `backend/internal/server/routes/gateway.go`、`handler/openai_gateway_count_tokens.go`、`service/openai_gateway_count_tokens.go`、`service/gateway_count_tokens.go` 與 `handler/gemini_v1beta_handler.go`。
