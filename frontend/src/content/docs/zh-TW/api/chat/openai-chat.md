## 端點與鑑權

```http
POST /v1/chat/completions
Authorization: Bearer $TAPMODELS_API_KEY
Content-Type: application/json
```

該端點接收 OpenAI Chat Completions 格式。模型必須屬於目前 API Key 的分組；閘道器可能原生轉發，也可能在 Chat Completions、Responses、Messages 或其他相容協議之間轉換。因此，請把本頁中的欄位視為閘道器接入契約，具體模型能力仍以 `GET /v1/models` 和最小實測請求為準。

## 核心請求參數

| 欄位 | 類型 | 必需 | 約束與說明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 精確模型 ID；空值返回 `invalid_request_error` |
| `messages` | array | 是 | 按時間順序排列的訊息；常用角色為 `system`、`developer`、`user`、`assistant`、`tool` |
| `messages[].content` | string / array / null | 視角色 | 文字可直接使用字串；多模態可使用內容塊。工具呼叫的 assistant 訊息可以為 `null` |
| `max_completion_tokens` | integer | 否 | 首選輸出 token 上限；舊用戶端可傳送 `max_tokens`。最終上限由模型和上游決定 |
| `temperature` / `top_p` | number | 否 | 取樣參數；範圍及推理模型是否忽略它們由目標模型決定 |
| `stream` | boolean | 否 | `true` 時返回 `text/event-stream`；必須是 JSON boolean |
| `stream_options.include_usage` | boolean | 否 | 請求流尾 usage；閘道器為計費可能向上游強制開啟，但用戶端仍應容忍 usage 缺失 |
| `tools` | array | 否 | 函式定義；每項使用 `type: "function"` 和 `function.{name,description,parameters}` |
| `tool_choice` | string / object | 否 | 常見值為 `auto`、`none`、`required`，或指定函式；可用值取決於處理鏈 |
| `parallel_tool_calls` | boolean | 否 | 是否允許一次返回多個工具呼叫；轉換鏈可能縮減語義 |
| `reasoning_effort` | string | 否 | 閘道器可解析 `low`、`medium`、`high`、`xhigh`；模型未必支援所有檔位 |
| `stop` | string / string[] | 否 | 停止序列；數量和長度限制由上游決定 |
| `response_format` | object | 否 | 支援 `json_object` 或 `json_schema` 的模型可用於結構化輸出 |

使用者訊息可包含 `text` 與 `image_url` 內容塊。程式碼中的相容類型也接受檔案內容塊，但 URL、data URI、檔案類型和大小能否使用仍取決於目標處理鏈。

## 最小請求

```bash
curl "$TAPMODELS_BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $TAPMODELS_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "YOUR_MODEL_ID",
    "messages": [{"role": "user", "content": "用一句話解釋冪等性。"}]
  }'
```

## 完整非串流回應

```json
{
  "id": "chatcmpl_example",
  "object": "chat.completion",
  "created": 1789401600,
  "model": "YOUR_MODEL_ID",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "冪等性表示同一操作重複執行多次，最終效果與執行一次相同。"
    },
    "finish_reason": "stop"
  }],
  "usage": {
    "prompt_tokens": 18,
    "completion_tokens": 24,
    "total_tokens": 42,
    "prompt_tokens_details": {"cached_tokens": 0},
    "completion_tokens_details": {"reasoning_tokens": 0}
  }
}
```

讀取 `choices[].message`，並根據 `finish_reason` 決定下一步：`stop` 表示自然結束，`length` 表示達到輸出限制，`tool_calls` 表示需要執行工具，`content_filter` 表示內容被攔截。相容轉換可能不保留所有上游專有欄位。

## SSE 串流回應

每個事件以 `data: {JSON}\n\n` 傳送，文字位於 `choices[].delta.content`。工具名和 `function.arguments` 也可能被拆成多個 delta，必須按 `choices[].index` 與 `tool_calls[].index` 累積，不能逐塊解析成完整 JSON。

```text
data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[{"index":0,"delta":{"content":"冪等性"},"finish_reason":null}]}

data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[],"usage":{"prompt_tokens":18,"completion_tokens":24,"total_tokens":42}}

data: [DONE]
```

以 `[DONE]`、最終 `finish_reason` 或帶 usage 的尾幀判斷正常結束。若連線在三者都沒有出現時斷開，應把結果標記為可能截斷並按業務冪等策略重試；不要把已收到的半段文字當成完整答案。HTTP 已開始傳送後，錯誤可能以流內事件或連線中斷出現。

## 工具呼叫閉環

首輪把函式 JSON Schema 放進 `tools`。若回應的 `finish_reason` 為 `tool_calls`，執行每個函式，再把原 assistant 訊息和每個結果一起發回：

```json
{
  "model": "YOUR_MODEL_ID",
  "messages": [
    {"role":"user","content":"上海現在幾點？"},
    {"role":"assistant","content":null,"tool_calls":[{
      "id":"call_1","type":"function",
      "function":{"name":"get_time","arguments":"{\"timezone\":\"Asia/Shanghai\"}"}
    }]},
    {"role":"tool","tool_call_id":"call_1","content":"{\"time\":\"10:30\"}"}
  ],
  "tools": [{"type":"function","function":{
    "name":"get_time",
    "description":"Return local time for an IANA timezone",
    "parameters":{"type":"object","properties":{"timezone":{"type":"string"}},"required":["timezone"],"additionalProperties":false}
  }}]
}
```

`function.arguments` 是字串，應先做 JSON 解析和業務驗證；不要直接執行模型生成的參數。每個 tool 結果的 `tool_call_id` 必須與呼叫 ID 匹配。

## 結構化輸出

支援該能力的模型可傳送：

```json
"response_format": {
  "type": "json_schema",
  "json_schema": {
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

閘道器能在部分協議轉換中對應 `response_format` 與 Responses 的 `text.format`，但不保證所有供應商執行嚴格 Schema。應用仍須解析 JSON、驗證 Schema，並處理拒絕、截斷或普通文字回應。

## 錯誤與故障排除

普通錯誤使用 `{"error":{"type":"...","message":"..."}}`。常見情況包括：401 API Key 無效；400 請求體、`model` 或 `stream` 類型錯誤；403 分組未開放能力；404 模型未在分組開放；429 額度或並行不足；502/503 上游或可排程帳號不可用。

故障排除時記錄回應中的 request ID 頭、HTTP 狀態和完整 `error.type/message`，但不要記錄 API Key、工具敏感參數或大型 data URI。先用同一 Key 呼叫 `GET /v1/models`，再縮減為本頁最小請求；模型在列表中只代表可見，不代表每個 Chat 參數都受支援。
