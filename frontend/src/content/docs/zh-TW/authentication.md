## 接入地址

本專案 的實際接入地址來自系統公開設定和控制台。原始 HTTP 請求使用 `{根地址}/v1/...`。OpenAI SDK 的 `base_url` 通常使用 `{根地址}/v1`；Anthropic SDK 使用根地址，由 SDK 新增 `/v1/messages`。

文件會對設定地址做歸一化，避免出現 `/v1/v1`。自訂部署網域、反向代理路徑和執行設定優先於範例預留位置網域。

## 推薦鑑權頭

| 協議 | 請求標頭 |
| --- | --- |
| OpenAI 相容 | `Authorization: Bearer $API_KEY` |
| Anthropic Messages | `x-api-key: $API_KEY` |
| Gemini 相容 | `x-goog-api-key: $API_KEY` |

一次請求只需要一種 API Key 鑑權方式。閘道器按有效 Bearer、`x-api-key`、`x-goog-api-key` 的順序讀取；Messages 原始請求還應帶適用的 `anthropic-version`。

## Key 與分組

Key 綁定的分組決定模型白名單、平台路由、費率、並行與其他策略。公開頁面能看到的模型廣場不等於任意 Key 的最終權限；使用 `GET /v1/models` 輔助發現，並以真實請求結果為準。

## 安全要求

- 只在伺服器端環境變數或金鑰管理服務中儲存 Key。
- 不要把 Key 放進 URL、瀏覽器前端、截圖、儲存庫或日誌。
- 洩漏後立即吊銷並建立新 Key。
- 401 `API_KEY_REQUIRED` 表示未提供 Key；無效 Key、分組無權限與上游鑑權失敗需要分別處理。
