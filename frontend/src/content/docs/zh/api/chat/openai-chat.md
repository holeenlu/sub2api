## 端点与鉴权

```http
POST /v1/chat/completions
Authorization: Bearer $API_KEY
Content-Type: application/json
```

该端点接收 OpenAI Chat Completions 格式。模型必须属于当前 API Key 的分组；网关可能原生转发，也可能在 Chat Completions、Responses、Messages 或其他兼容协议之间转换。因此，请把本页中的字段视为网关接入契约，具体模型能力仍以 `GET /v1/models` 和最小实测请求为准。

## 核心请求参数

| 字段 | 类型 | 必需 | 约束与说明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 精确模型 ID；空值返回 `invalid_request_error` |
| `messages` | array | 是 | 按时间顺序排列的消息；常用角色为 `system`、`developer`、`user`、`assistant`、`tool` |
| `messages[].content` | string / array / null | 视角色 | 文本可直接使用字符串；多模态可使用内容块。工具调用的 assistant 消息可以为 `null` |
| `max_completion_tokens` | integer | 否 | 首选输出 token 上限；旧客户端可发送 `max_tokens`。最终上限由模型和上游决定 |
| `temperature` / `top_p` | number | 否 | 采样参数；范围及推理模型是否忽略它们由目标模型决定 |
| `stream` | boolean | 否 | `true` 时返回 `text/event-stream`；必须是 JSON boolean |
| `stream_options.include_usage` | boolean | 否 | 请求流尾 usage；网关为计费可能向上游强制开启，但客户端仍应容忍 usage 缺失 |
| `tools` | array | 否 | 函数定义；每项使用 `type: "function"` 和 `function.{name,description,parameters}` |
| `tool_choice` | string / object | 否 | 常见值为 `auto`、`none`、`required`，或指定函数；可用值取决于处理链 |
| `parallel_tool_calls` | boolean | 否 | 是否允许一次返回多个工具调用；转换链可能缩减语义 |
| `reasoning_effort` | string | 否 | 网关可解析 `low`、`medium`、`high`、`xhigh`；模型未必支持所有档位 |
| `stop` | string / string[] | 否 | 停止序列；数量和长度限制由上游决定 |
| `response_format` | object | 否 | 支持 `json_object` 或 `json_schema` 的模型可用于结构化输出 |

用户消息可包含 `text` 与 `image_url` 内容块。代码中的兼容类型也接受文件内容块，但 URL、data URI、文件类型和大小能否使用仍取决于目标处理链。

## 最小请求

```bash
curl "$API_BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "YOUR_MODEL_ID",
    "messages": [{"role": "user", "content": "用一句话解释幂等性。"}]
  }'
```

## 完整非流式响应

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
      "content": "幂等性表示同一操作重复执行多次，最终效果与执行一次相同。"
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

读取 `choices[].message`，并根据 `finish_reason` 决定下一步：`stop` 表示自然结束，`length` 表示达到输出限制，`tool_calls` 表示需要执行工具，`content_filter` 表示内容被拦截。兼容转换可能不保留所有上游专有字段。

## SSE 流式响应

每个事件以 `data: {JSON}\n\n` 发送，文本位于 `choices[].delta.content`。工具名和 `function.arguments` 也可能被拆成多个 delta，必须按 `choices[].index` 与 `tool_calls[].index` 累积，不能逐块解析成完整 JSON。

```text
data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[{"index":0,"delta":{"content":"幂等性"},"finish_reason":null}]}

data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[],"usage":{"prompt_tokens":18,"completion_tokens":24,"total_tokens":42}}

data: [DONE]
```

以 `[DONE]`、最终 `finish_reason` 或带 usage 的尾帧判断正常结束。若连接在三者都没有出现时断开，应把结果标记为可能截断并按业务幂等策略重试；不要把已收到的半段文本当成完整答案。HTTP 已开始发送后，错误可能以流内事件或连接中断出现。

## 工具调用闭环

首轮把函数 JSON Schema 放进 `tools`。若响应的 `finish_reason` 为 `tool_calls`，执行每个函数，再把原 assistant 消息和每个结果一起发回：

```json
{
  "model": "YOUR_MODEL_ID",
  "messages": [
    {"role":"user","content":"上海现在几点？"},
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

`function.arguments` 是字符串，应先做 JSON 解析和业务校验；不要直接执行模型生成的参数。每个 tool 结果的 `tool_call_id` 必须与调用 ID 匹配。

## 结构化输出

支持该能力的模型可发送：

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

网关能在部分协议转换中映射 `response_format` 与 Responses 的 `text.format`，但不保证所有供应商执行严格 Schema。应用仍须解析 JSON、验证 Schema，并处理拒绝、截断或普通文本响应。

## 错误与排障

普通错误使用 `{"error":{"type":"...","message":"..."}}`。常见情况包括：401 API Key 无效；400 请求体、`model` 或 `stream` 类型错误；403 分组未开放能力；404 模型未在分组开放；429 额度或并发不足；502/503 上游或可调度账号不可用。

排障时记录响应中的 request ID 头、HTTP 状态和完整 `error.type/message`，但不要记录 API Key、工具敏感参数或大型 data URI。先用同一 Key 调用 `GET /v1/models`，再缩减为本页最小请求；模型在列表中只代表可见，不代表每个 Chat 参数都受支持。
