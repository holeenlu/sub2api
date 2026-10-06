## Grok 專屬協議面

Grok 文字走 OpenAI 相容的 Chat Completions、Responses 和 Messages 橋接；本頁只列 Grok 分組特有的媒體、搜尋和語音端點。

| 功能 | 端點 | 邊界 |
| --- | --- | --- |
| 影像 | `POST /v1/images/generations`、`/edits` | Grok 分組，需影像權限和媒體帳號能力 |
| 非同步影像 | 同步路徑追加 `/async`，輪詢 `GET /v1/images/tasks/{task_id}` | OpenAI/Grok 分組且物件儲存已啟用 |
| 影片 | `/v1/videos`、`/videos/generations`、`/edits`、`/extensions` | 生成/查詢可允許解析到 Grok 的複合分組；編輯/擴充套件要求 Grok |
| 搜尋 | `POST /v1/web_search`、`POST /v1/x_search` | 僅 Grok 分組 |
| HTTP 語音 | `/v1/tts`、`/v1/stt`、`/v1/custom-voices...` | 僅 Grok 分組，轉發原生請求/回應 |
| Realtime 語音 | `GET /v1/realtime?model=...` | 僅 Grok，必須 WebSocket Upgrade |

Bearer Key 是所有 HTTP 端點的建議鑑權。`GET /v1/models` 可見不等於媒體帳號具備相應能力。

## 影像與影片

Grok 影像複用 OpenAI Images 路徑，JSON/multipart 可攜帶 `model`、`prompt`、`n`、`size`、`aspect_ratio`、`resolution` 和輸入圖片；編輯最多三個源圖。具體尺寸、比例、數量、mask 語義由上游模型決定。

影片端點：

```http
POST /v1/videos
POST /v1/videos/generations
POST /v1/videos/edits
POST /v1/videos/extensions
GET  /v1/videos/{request_id}
GET  /v1/videos/{request_id}/content
```

請求通常需要模型和提示，可能包含 `resolution`、`duration`、`aspect_ratio`、輸入影像 URL。建立返回 request ID，輪詢狀態別名；僅 `status: "done"` 且 `video.url` 非空被認定完成，content 路徑通過建立任務綁定帳號代理下載。

## 獨立搜尋

```bash
curl "$TAPMODELS_BASE_URL/v1/web_search" \
  -H "Authorization: Bearer $TAPMODELS_API_KEY" -H "Content-Type: application/json" \
  -d '{"query":"TapModels API updates","max_results":5}'
```

`query` 必填，也支援 `input`；`max_results` 預設 5、上限 20。`/v1/x_search` 另支援 `allowed_x_handles`、`excluded_x_handles`、`from_date`、`to_date`、`enable_image_understanding`、`enable_video_understanding`。回應是閘道器聚合的 `query/results/provider/max_results`，不是 Responses 工具事件原樣透傳；應用應驗證 URL 和去重。

## 語音與 Realtime

TTS、STT、自訂聲音保留用戶端 Content-Type 並轉發原生 body/回應；欄位和媒體類型由上游決定。自訂聲音支援列表、建立、讀取、更新、刪除和音訊讀取。Realtime 的 `model` 預設 `grok-voice-latest`，必須 Upgrade；普通 GET 返回 426。

非 Grok 分組存取專屬端點通常返回 404 `not_found_error`。其他錯誤包括空體、能力不匹配、無帳號、額度/並行和上游 4xx/5xx。原始碼對應 `routes/gateway.go`、`handler/grok_media.go`、`service/grok_media.go`、`handler/gateway_web_search.go`、`handler/openai_x_search.go`、`handler/grok_audio.go`。
