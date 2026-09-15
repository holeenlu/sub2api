## 準備工作

在控制台建立 API Key，並確認它所屬的分組。分組決定可以使用的模型、倍率、限額與上游路由；不要僅憑公開模型頁判斷一把 Key 是否可用。

從控制台複製接入地址。範例中的 `TAPMODELS_BASE_URL` 表示不含末尾 `/v1` 的根地址；若複製的地址已經包含 `/v1`，先移除它再拼接具體路徑。

```bash
export TAPMODELS_BASE_URL="https://你的接入網域"
export TAPMODELS_API_KEY="你的金鑰"
```

## 選擇協議

| 現有用戶端或需求 | 推薦入口 |
| --- | --- |
| OpenAI Responses SDK 或工具工作流 | `POST /v1/responses` |
| OpenAI Chat Completions 用戶端 | `POST /v1/chat/completions` |
| Anthropic SDK、Claude Code 或 Messages 請求體 | `POST /v1/messages` |
| 影像生成或編輯 | `POST /v1/images/generations`、`POST /v1/images/edits` |

## 執行請求

在下方範例中選擇目前分組開放的模型。HTTP 200 只表示該次請求成功；回應裡的模型、`usage`、快取欄位和費用記錄仍應核對。

## 核對結果

1. 儲存伺服器端回傳的 request ID，便於檢查。
2. 從協議對應欄位讀取文字、工具呼叫或圖片結果。
3. 在控制台用量記錄核對模型 ID、輸入、輸出、快取、倍率和實際費用。
4. 收到 404 模型錯誤時，先檢查 Key 分組和模型 ID，再檢查端點是否適用於該分組。

## 下一步

先閱讀[鑑權與接入地址](/docs/authentication)，再進入對應協議頁。生產程式碼應設定逾時、處理 429/5xx，並避免把 API Key 寫入日誌。
