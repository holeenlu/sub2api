## 端點與鑑權

```http
POST /v1/images/generations
POST /v1/images/edits
Authorization: Bearer $TAPMODELS_API_KEY
```

目前產品展示的影像模型包括 `gpt-image-2.5-flare` 與 `gpt-image-2.5-sunburst`，實際權限由 API Key 分組和執行中的相容帳號決定。閘道器驗證 `gpt-image-*` 模型族；如果省略 `model`，程式碼預設使用 `gpt-image-2`，但預設值不代表目前分組一定可排程，生產整合應始終顯式傳送 `GET /v1/models` 回傳的精確 ID。

## 生成請求

生成介面使用 JSON：

```bash
curl "$TAPMODELS_BASE_URL/v1/images/generations" \
  -H "Authorization: Bearer $TAPMODELS_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model":"gpt-image-2.5-flare",
    "prompt":"A clean product photo of a red desk lamp on a white background",
    "n":1,
    "response_format":"b64_json"
  }'
```

| 欄位 | 類型 | 必需 | 閘道器行為與限制 |
| --- | --- | --- | --- |
| `model` | string | 建議是 | 必須屬於 `gpt-image-*` 或已實現的 Grok 影像族；省略時預設 `gpt-image-2` |
| `prompt` | string | 是 | 生成或編輯指令；空提示最終會被目標鏈拒絕 |
| `n` | integer | 否 | 預設 1，閘道器要求大於 0；最大值由上游決定 |
| `size` | string | 否 | 可傳上游支援的尺寸或檔位；計費歸一化識別 1K/2K/4K，實際畫素以結果檔案為準 |
| `quality` | string | 否 | 原生選項；允許值由精確模型決定 |
| `response_format` | string | 否 | `b64_json` 或目標鏈支援的 `url`；URL 可能有有效期 |
| `background` | string | 否 | 背景選項；需模型支援 |
| `output_format` | string | 否 | 輸出編碼；需模型支援 |
| `output_compression` | integer | 否 | 壓縮設定；閘道器驗證類型，上限由上游決定 |
| `moderation` / `style` | string | 否 | 原生選項；並非所有模型支援 |
| `partial_images` | integer | 否 | 串流部分圖數量；需原生能力 |
| `stream` | boolean | 否 | `true` 返回影像 SSE；必須是 boolean |

不要從其他 GPT Image 版本推斷 `quality`、尺寸、格式或多圖上限。顯式模型、顯式尺寸、`stream`、`n != 1`、mask 以及上述原生選項都會要求具備 `images-native` 能力的帳號。

## 影像編輯：multipart/form-data

編輯的檔案上傳形式接受一個或多個 `image` / `image[n]` part，以及可選 `mask`。每個上傳 part 最多讀取 20 MiB；總請求大小還受伺服器端閘道器設定限制。

```bash
curl "$TAPMODELS_BASE_URL/v1/images/edits" \
  -H "Authorization: Bearer $TAPMODELS_API_KEY" \
  -F "model=gpt-image-2.5-flare" \
  -F "prompt=Replace the background with a quiet library" \
  -F "image=@input.png;type=image/png" \
  -F "mask=@mask.png;type=image/png" \
  -F "response_format=b64_json"
```

multipart 必須帶 boundary；編輯缺少圖片會返回 `image file is required`。`mask` 會作為獨立圖片上傳，具體透明區域語義、尺寸匹配和檔案格式由目標模型驗證。閘道器不會僅憑 part header 預先得出影像寬高。

JSON 編輯也受支援，格式為 `"images":[{"image_url":"https://..."}]`，mask 使用 `{"image_url":"..."}`。至少需要一個 `images[].image_url`；`images[].file_id` 和 `mask.file_id` 會被明確拒絕。

## 非串流回應

```json
{
  "created": 1789401600,
  "data": [{
    "b64_json": "iVBORw0KGgoAAA...",
    "revised_prompt": "A clean product photo of a red desk lamp on white."
  }],
  "usage": {
    "input_tokens": 120,
    "output_tokens": 1056,
    "total_tokens": 1176
  }
}
```

結果也可能在 `data[].url`。只解碼 JSON 中的 `b64_json` 欄位，先驗證 base64、MIME 和真實寬高；不要把大型 payload 寫進日誌。若請求 `url`，閘道器在部分處理鏈會把 data URI 或下載結果回填，仍應把 URL 當作短期結果並及時轉存。

## 影像 SSE

原生流會產生 `image_generation.partial_image` 和 `image_generation.completed`（編輯路徑的下游事件名可能歸一化為 `image_edit.*`），payload 可包含 `b64_json`、`partial_image_index`、`size`、`output_format` 和 usage。不同帳號也可能經 Responses 事件轉換，因此用戶端應以 JSON 的 `type` 為準，相容 `response.*` 和 `response.image_generation_call.*` 事件。

只有收到 completed 影像事件或帶有效影像結果的最終回應才算成功。`response.incomplete`、`response.failed`、`error`、空 output 或完成前斷流均應視為失敗/未知，不能僅因 HTTP 已是 200 就儲存結果。

## 非同步提交與輪詢

非同步功能依賴管理員啟用物件儲存；關閉時提交返回 404，但已建立任務在任務儲存可用時仍可輪詢。非同步請求與同步端點使用相同 JSON 或 multipart payload，且禁止 `stream: true`。

```http
POST /v1/images/generations/async
POST /v1/images/edits/async
GET  /v1/images/tasks/{task_id}
```

提交成功返回 HTTP 202，並帶 `Location: /v1/images/tasks/{task_id}`、`Retry-After: 3` 和：

```json
{
  "id":"imgtask_abc123",
  "task_id":"imgtask_abc123",
  "object":"image.generation.task",
  "status":"processing",
  "created_at":1789401600,
  "expires_at":1789488000,
  "poll_url":"/v1/images/tasks/imgtask_abc123"
}
```

使用提交時同一把 API Key 輪詢。目前實際狀態只有 `processing`、`completed`、`failed`：

```json
{
  "id":"imgtask_abc123",
  "task_id":"imgtask_abc123",
  "object":"image.generation.task",
  "status":"completed",
  "http_status":200,
  "image_url":"https://storage.example/result.png",
  "result":{"created":1789401601,"data":[{"url":"https://storage.example/result.png"}]},
  "created_at":1789401600,
  "completed_at":1789401601,
  "expires_at":1789488001
}
```

`processing` 回應帶 `Retry-After: 3`；`failed` 返回原同步呼叫的 `http_status` 和 `error`。任務按使用者及 API Key 雙重歸屬，其他 Key 查詢同一 ID 返回 404。預設結果 TTL 為 24 小時，執行逾時為 30 分鐘；部署設定可以覆蓋，不應把這兩個預設值當成永久 SLA。此介面目前不提供 webhook。

## 錯誤與故障排除

錯誤使用 `{"error":{"type":"...","message":"..."}}`；非同步介面同時回顯 `error.code`。重點檢查：模型是否屬於影像族；分組是否開放影像權限；編輯請求的 Content-Type/boundary 和圖片欄位；`n`/`stream`/壓縮欄位類型；非同步物件儲存是否啟用；輪詢是否使用原 API Key。

遇到 502/503、空 output 或流內 error 時保留 request ID、模型、端點、HTTP 狀態和最後事件類型。不要記錄 API Key、原圖 base64、mask 或生成結果正文。
