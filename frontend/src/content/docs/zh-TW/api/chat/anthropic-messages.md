## 端點與鑑權

```http
POST /v1/messages
x-api-key: $KDAN_API_KEY
anthropic-version: 2023-06-01
Content-Type: application/json
```

閘道器也接受 Bearer 鑑權，但 Anthropic SDK 和 Claude Code 建議保持 `x-api-key`。Anthropic 分組通常走 Messages 相容鏈；OpenAI、Grok、Kimi、智譜、DeepSeek、MiniMax 和 OpenCodeGo 分組可能橋接到 OpenAI 閘道器。跨協議時內容塊、錯誤、停止原因和 usage 不保證逐欄位等價。

輸入 token 預估使用 `POST /v1/messages/count_tokens`，請求體沿用本頁 Messages 形狀並返回 `{"input_tokens":...}`。Grok 和部分 OpenAI 相容鏈可能使用本地 tokenizer，因此不要把該值當作最終 usage 或帳單。

## 核心請求參數

| 欄位 | 類型 | 必需 | 約束與說明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 目前分組開放的精確模型 ID |
| `max_tokens` | integer | 是 | 最大輸出 token；不能超過模型或分組限制 |
| `messages` | array | 是 | `user` 與 `assistant` 訊息序列；系統指令放頂層 `system` |
| `messages[].content` | string / array | 是 | 支援文字及處理鏈已實現的影像、thinking、tool_use、tool_result 塊 |
| `system` | string / array | 否 | 頂層指令；陣列塊可帶已支援的 `cache_control` |
| `temperature` / `top_p` | number | 否 | 是否支援及範圍由目標模型決定 |
| `stop_sequences` | string[] | 否 | 自訂停止序列 |
| `thinking` | object | 否 | 閘道器可解析 `enabled`、`adaptive`、`disabled`；`enabled` 可帶 `budget_tokens` |
| `output_config.effort` | string | 否 | 可解析 `low/medium/high/max`，仍需模型支援 |
| `tools` | array | 否 | 每項通常含 `name`、`description`、`input_schema` |
| `tool_choice` | object | 否 | 自動、任意或指定工具；兼容範圍依處理鏈 |
| `stream` | boolean | 否 | `true` 返回 Anthropic SSE |

影像塊使用 `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"..."}}`。快取控制程式碼可解析 `{"type":"ephemeral","ttl":"5m"}` 或 `1h`，但快取是否命中、最小字首和計費以具體模型及價格頁為準。

## 最小請求與非流回應

```bash
curl "$KDAN_BASE_URL/v1/messages" \
  -H "x-api-key: $KDAN_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model":"YOUR_MODEL_ID",
    "max_tokens":256,
    "messages":[{"role":"user","content":"用一句話解釋冪等性。"}]
  }'
```

```json
{
  "id": "msg_example",
  "type": "message",
  "role": "assistant",
  "content": [{
    "type": "text",
    "text": "冪等性表示同一操作重複執行多次，最終效果與執行一次相同。"
  }],
  "model": "YOUR_MODEL_ID",
  "stop_reason": "end_turn",
  "stop_sequence": null,
  "usage": {
    "input_tokens": 18,
    "output_tokens": 24,
    "cache_creation_input_tokens": 0,
    "cache_read_input_tokens": 0
  }
}
```

遍歷 `content` 並按塊類型處理，不能只讀取第一個文字塊。`stop_reason` 常見為 `end_turn`、`max_tokens`、`stop_sequence`、`tool_use`；實際值可能受協議轉換影響。

## SSE 事件與結束處理

正常事件順序為 `message_start`，一個或多個內容塊的 `content_block_start`、`content_block_delta`、`content_block_stop`，然後 `message_delta` 和 `message_stop`。delta 類型包括 `text_delta`、`thinking_delta`、`signature_delta` 和工具參數的 `input_json_delta`。

```text
event: message_start
data: {"type":"message_start","message":{"id":"msg_example","type":"message","role":"assistant","content":[],"model":"YOUR_MODEL_ID","stop_reason":null,"usage":{"input_tokens":18,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"冪等性"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":24}}

event: message_stop
data: {"type":"message_stop"}
```

按 `index` 累積塊；`input_json_delta.partial_json` 只是片段，必須等對應 `content_block_stop` 後再解析。以 `message_stop` 為正常結束。若提前斷開或收到 `event: error`，不要把未閉合的文字、thinking 或工具參數當成完整結果。

## 工具呼叫閉環

模型返回 `tool_use` 塊後，應用執行工具，並在下一條 `user` 訊息裡回傳匹配的 `tool_result`：

```json
{
  "model":"YOUR_MODEL_ID",
  "max_tokens":256,
  "messages":[
    {"role":"user","content":"上海現在幾點？"},
    {"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"get_time","input":{"timezone":"Asia/Shanghai"}}]},
    {"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"{\"time\":\"10:30\"}"}]}
  ],
  "tools":[{
    "name":"get_time",
    "description":"Return local time for an IANA timezone",
    "input_schema":{"type":"object","properties":{"timezone":{"type":"string"}},"required":["timezone"],"additionalProperties":false}
  }]
}
```

`tool_use_id` 必須匹配原呼叫。工具輸入由模型生成，執行前應做 Schema 和權限驗證；工具錯誤可用 `is_error: true` 回傳。

## 結構化輸出與相容邊界

Messages 的核心結構化機制是工具 `input_schema`。如果目標模型支援原生結構化輸出，可使用其明確支援的參數；不要把 Chat Completions 的 `response_format` 或 Responses 的 `text.format` 直接複製到本端點。跨協議橋接會盡力轉換函式、思考與停止原因，但供應商專有 server tools、快取語義和加密 reasoning 往返需要按目標分組實測。

## 錯誤與故障排除

Messages 錯誤形如 `{"type":"error","error":{"type":"invalid_request_error","message":"..."}}`。檢查 HTTP 狀態、request ID、`error.type` 和訊息：401 多為 Key 問題；400 多為請求體、模型或內容塊錯誤；403/404 多為分組權限或模型不可用；429 為額度/並行限制；502/503 為上游或排程容量問題。

先用同一 Key 呼叫 `GET /v1/models`，再縮減為最小請求。串流問題同時記錄最後完整的事件類型和塊 index；不要記錄 API Key、完整 base64 影像、thinking 內容或敏感工具輸入。
