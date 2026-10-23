## 端點與鑑權

```http
POST /v1/responses
Authorization: Bearer $API_KEY
Content-Type: application/json
```

這是 OpenAI Responses 風格入口。閘道器會按 Key 分組選擇原生或跨協議處理鏈；官方 Responses API 出現的內建工具並不因此全部可用。

只計算輸入時使用 `POST /v1/responses/input_tokens`。它要求非空 `model`，返回 `{"object":"response.input_tokens","input_tokens":...}`；部分帳號會本地估算，因此不能替代實際生成 usage。

## 核心請求參數

| 欄位 | 類型 | 必需 | 約束與說明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 目前分組開放的精確模型 ID |
| `input` | string / array | 是 | 簡單文字，或訊息、函式呼叫與函式結果組成的類型化陣列 |
| `instructions` | string | 否 | 本次回應的頂層指令 |
| `max_output_tokens` | integer | 否 | 轉換實現的安全下限為 128；更小值可能被提升或被目標鏈拒絕 |
| `temperature` / `top_p` | number | 否 | 是否生效及範圍由模型決定 |
| `reasoning` | object | 否 | `effort` 可解析 `low/medium/high/xhigh`；`summary` 可使用 `auto/concise/detailed`，仍需模型支援 |
| `text` | object | 否 | `format` 設定結構化輸出；`verbosity` 可解析 `low/medium/high` |
| `tools` | array | 否 | 閘道器類型包含 function、custom 及若干用戶端/搜尋工具，但可用性取決於分組和處理鏈 |
| `tool_choice` | string / object | 否 | 自動、停用、強制或指定工具；跨協議時可能縮減 |
| `parallel_tool_calls` | boolean | 否 | 是否允許並行工具呼叫 |
| `previous_response_id` | string | 否 | 必須是目前使用者可用的 `resp_*`；不能傳 message ID |
| `include` | string[] | 否 | 請求額外欄位；僅已實現的上游能力有效 |
| `store` | boolean | 否 | 透傳意圖不等於本站提供回應歷史讀取 API |
| `stream` | boolean | 否 | `true` 返回 Responses SSE 事件；必須是 JSON boolean |

陣列輸入的常見內容塊包括 `input_text`、`input_image` 和 `input_file`。影像使用 `image_url`，檔案可使用 `file_data` 或 `file_id`，但具體來源、類型與大小限制由目標模型和上游決定。

## 最小請求與非流回應

```bash
curl "$API_BASE_URL/v1/responses" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","input":"用一句話解釋冪等性。"}'
```

```json
{
  "id": "resp_example",
  "object": "response",
  "created_at": 1789401600,
  "model": "YOUR_MODEL_ID",
  "status": "completed",
  "output": [{
    "id": "msg_example",
    "type": "message",
    "role": "assistant",
    "status": "completed",
    "content": [{
      "type": "output_text",
      "text": "冪等性表示同一操作重複執行多次，最終效果與執行一次相同。"
    }]
  }],
  "usage": {
    "input_tokens": 18,
    "output_tokens": 24,
    "total_tokens": 42,
    "input_tokens_details": {"cached_tokens": 0},
    "output_tokens_details": {"reasoning_tokens": 0}
  }
}
```

不要假定 `output[0]` 是文字。遍歷 `output` 並按 `type` 處理 `message`、`reasoning`、`function_call`、`custom_tool_call`、`web_search_call` 等項。僅當 `status` 為 `completed` 時按完整成功處理；`incomplete` 檢視 `incomplete_details.reason`，`failed` 檢視 `error`。

## SSE 事件與結束處理

串流回應以事件類型驅動。用戶端至少應支援：

| 事件 | 用途 |
| --- | --- |
| `response.created` / `response.in_progress` | 初始化回應 ID 與狀態 |
| `response.output_item.added` / `.done` | 開始或完成一個類型化輸出項 |
| `response.content_part.added` / `.done` | 內容塊生命週期 |
| `response.output_text.delta` / `.done` | 累積文字 |
| `response.function_call_arguments.delta` / `.done` | 累積工具參數字串 |
| `response.reasoning_summary_text.delta` / `.done` | 累積推理摘要（如模型提供） |
| `response.completed` | 成功終止；最終 `response` 可含完整 output 與 usage |
| `response.incomplete` / `response.failed` / `error` | 非成功終止 |

```text
event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_example","output_index":0,"content_index":0,"delta":"冪等性"}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_example","object":"response","created_at":1789401600,"model":"YOUR_MODEL_ID","status":"completed","output":[],"usage":{"input_tokens":18,"output_tokens":24,"total_tokens":42}}}
```

以終止事件中的狀態為準。不要只等 `[DONE]`：Responses 的完成語義是 `response.completed`，失敗和不完整也有各自事件。連線在終止事件前中斷時，將本輪視為未知/可能截斷，不要自動執行尚未完成的工具參數。

## 函式工具回合

首輪定義函式。模型回傳的 `function_call` 項包含 `call_id`、`name` 和字串 `arguments`。執行並驗證參數後，在下一次請求中回傳呼叫項和結果：

```json
{
  "model": "YOUR_MODEL_ID",
  "input": [
    {"type":"function_call","call_id":"call_1","name":"get_time","arguments":"{\"timezone\":\"Asia/Shanghai\"}"},
    {"type":"function_call_output","call_id":"call_1","output":"{\"time\":\"10:30\"}"}
  ],
  "tools": [{
    "type":"function",
    "name":"get_time",
    "description":"Return local time for an IANA timezone",
    "parameters":{"type":"object","properties":{"timezone":{"type":"string"}},"required":["timezone"],"additionalProperties":false},
    "strict":true
  }]
}
```

HTTP 請求中的 `function_call_output` 必須帶 `call_id`。閘道器不會把缺少呼叫關聯的結果當成合法續輪；僅 Responses WebSocket v2 對 continuation 有不同處理。

## 結構化輸出

結構化格式位於 `text.format`，不是 Chat Completions 的 `response_format`：

```json
"text": {
  "format": {
    "type": "json_schema",
    "name": "answer",
    "strict": true,
    "schema": {
      "type": "object",
      "properties": {"answer": {"type": "string"}},
      "required": ["answer"],
      "additionalProperties": false
    }
  }
}
```

轉換鏈可以對應常見 JSON 格式，但嚴格約束最終仍由模型執行。用戶端必須驗證解析結果，並處理拒絕、`incomplete` 與普通文字回退。

## WebSocket 與子路徑邊界

`GET /v1/responses` 是需要 `Upgrade: websocket` 的入口，不是 Retrieve Response；普通 GET 會返回 426。`POST /v1/responses/*subpath` 只接受閘道器守衛允許的子路徑，不應據此假設官方所有 Responses CRUD 介面均已實現。常規整合優先使用本頁的 HTTP/SSE。

## 錯誤與故障排除

普通 HTTP 錯誤形如 `{"error":{"type":"invalid_request_error","message":"..."}}`。常見原因包括 Key 無效、模型缺失、`stream` 類型錯誤、`previous_response_id` 不是 `resp_*` 或不屬於目前使用者、模型未開放、額度/並行不足以及上游不可用。

記錄 HTTP 狀態、request ID、事件類型和最終 `response.status`。不要只記錄最後一段文字；對工具工作流還應記錄 `call_id`，但應去識別化工具輸入。模型列表可見性不代表官方內建工具、儲存、子路徑或每個 `include` 值均受支援。
