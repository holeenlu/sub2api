## 端点与鉴权

```http
POST /v1/messages
x-api-key: $API_KEY
anthropic-version: 2023-06-01
Content-Type: application/json
```

网关也接受 Bearer 鉴权，但 Anthropic SDK 和 Claude Code 建议保持 `x-api-key`。Anthropic 分组通常走 Messages 兼容链；OpenAI、Grok、Kimi、智谱、DeepSeek、MiniMax 和 OpenCodeGo 分组可能桥接到 OpenAI 网关。跨协议时内容块、错误、停止原因和 usage 不保证逐字段等价。

输入 token 预估使用 `POST /v1/messages/count_tokens`，请求体沿用本页 Messages 形状并返回 `{"input_tokens":...}`。Grok 和部分 OpenAI 兼容链可能使用本地 tokenizer，因此不要把该值当作最终 usage 或账单。

## 核心请求参数

| 字段 | 类型 | 必需 | 约束与说明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 当前分组开放的精确模型 ID |
| `max_tokens` | integer | 是 | 最大输出 token；不能超过模型或分组限制 |
| `messages` | array | 是 | `user` 与 `assistant` 消息序列；系统指令放顶层 `system` |
| `messages[].content` | string / array | 是 | 支持文本及处理链已实现的图像、thinking、tool_use、tool_result 块 |
| `system` | string / array | 否 | 顶层指令；数组块可带已支持的 `cache_control` |
| `temperature` / `top_p` | number | 否 | 是否支持及范围由目标模型决定 |
| `stop_sequences` | string[] | 否 | 自定义停止序列 |
| `thinking` | object | 否 | 网关可解析 `enabled`、`adaptive`、`disabled`；`enabled` 可带 `budget_tokens` |
| `output_config.effort` | string | 否 | 可解析 `low/medium/high/max`，仍需模型支持 |
| `tools` | array | 否 | 每项通常含 `name`、`description`、`input_schema` |
| `tool_choice` | object | 否 | 自动、任意或指定工具；兼容范围依处理链 |
| `stream` | boolean | 否 | `true` 返回 Anthropic SSE |

图像块使用 `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"..."}}`。缓存控制代码可解析 `{"type":"ephemeral","ttl":"5m"}` 或 `1h`，但缓存是否命中、最小前缀和计费以具体模型及价格页为准。

## 最小请求与非流响应

```bash
curl "$API_BASE_URL/v1/messages" \
  -H "x-api-key: $API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model":"YOUR_MODEL_ID",
    "max_tokens":256,
    "messages":[{"role":"user","content":"用一句话解释幂等性。"}]
  }'
```

```json
{
  "id": "msg_example",
  "type": "message",
  "role": "assistant",
  "content": [{
    "type": "text",
    "text": "幂等性表示同一操作重复执行多次，最终效果与执行一次相同。"
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

遍历 `content` 并按块类型处理，不能只读取第一个文本块。`stop_reason` 常见为 `end_turn`、`max_tokens`、`stop_sequence`、`tool_use`；实际值可能受协议转换影响。

## SSE 事件与结束处理

正常事件顺序为 `message_start`，一个或多个内容块的 `content_block_start`、`content_block_delta`、`content_block_stop`，然后 `message_delta` 和 `message_stop`。delta 类型包括 `text_delta`、`thinking_delta`、`signature_delta` 和工具参数的 `input_json_delta`。

```text
event: message_start
data: {"type":"message_start","message":{"id":"msg_example","type":"message","role":"assistant","content":[],"model":"YOUR_MODEL_ID","stop_reason":null,"usage":{"input_tokens":18,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"幂等性"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":24}}

event: message_stop
data: {"type":"message_stop"}
```

按 `index` 累积块；`input_json_delta.partial_json` 只是片段，必须等对应 `content_block_stop` 后再解析。以 `message_stop` 为正常结束。若提前断开或收到 `event: error`，不要把未闭合的文本、thinking 或工具参数当成完整结果。

## 工具调用闭环

模型返回 `tool_use` 块后，应用执行工具，并在下一条 `user` 消息里回传匹配的 `tool_result`：

```json
{
  "model":"YOUR_MODEL_ID",
  "max_tokens":256,
  "messages":[
    {"role":"user","content":"上海现在几点？"},
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

`tool_use_id` 必须匹配原调用。工具输入由模型生成，执行前应做 Schema 和权限校验；工具错误可用 `is_error: true` 回传。

## 结构化输出与兼容边界

Messages 的核心结构化机制是工具 `input_schema`。如果目标模型支持原生结构化输出，可使用其明确支持的参数；不要把 Chat Completions 的 `response_format` 或 Responses 的 `text.format` 直接复制到本端点。跨协议桥接会尽力转换函数、思考与停止原因，但供应商专有 server tools、缓存语义和加密 reasoning 往返需要按目标分组实测。

## 错误与排障

Messages 错误形如 `{"type":"error","error":{"type":"invalid_request_error","message":"..."}}`。检查 HTTP 状态、request ID、`error.type` 和消息：401 多为 Key 问题；400 多为请求体、模型或内容块错误；403/404 多为分组权限或模型不可用；429 为额度/并发限制；502/503 为上游或调度容量问题。

先用同一 Key 调用 `GET /v1/models`，再缩减为最小请求。流式问题同时记录最后完整的事件类型和块 index；不要记录 API Key、完整 base64 图像、thinking 内容或敏感工具输入。
