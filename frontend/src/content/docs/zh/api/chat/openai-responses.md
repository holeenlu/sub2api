## 端点与鉴权

```http
POST /v1/responses
Authorization: Bearer $KDAN_API_KEY
Content-Type: application/json
```

这是 OpenAI Responses 风格入口。网关会按 Key 分组选择原生或跨协议处理链；官方 Responses API 出现的内置工具并不因此全部可用。

只计算输入时使用 `POST /v1/responses/input_tokens`。它要求非空 `model`，返回 `{"object":"response.input_tokens","input_tokens":...}`；部分账号会本地估算，因此不能替代实际生成 usage。

## 核心请求参数

| 字段 | 类型 | 必需 | 约束与说明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 当前分组开放的精确模型 ID |
| `input` | string / array | 是 | 简单文本，或消息、函数调用与函数结果组成的类型化数组 |
| `instructions` | string | 否 | 本次响应的顶层指令 |
| `max_output_tokens` | integer | 否 | 转换实现的安全下限为 128；更小值可能被提升或被目标链拒绝 |
| `temperature` / `top_p` | number | 否 | 是否生效及范围由模型决定 |
| `reasoning` | object | 否 | `effort` 可解析 `low/medium/high/xhigh`；`summary` 可使用 `auto/concise/detailed`，仍需模型支持 |
| `text` | object | 否 | `format` 配置结构化输出；`verbosity` 可解析 `low/medium/high` |
| `tools` | array | 否 | 网关类型包含 function、custom 及若干客户端/搜索工具，但可用性取决于分组和处理链 |
| `tool_choice` | string / object | 否 | 自动、禁用、强制或指定工具；跨协议时可能缩减 |
| `parallel_tool_calls` | boolean | 否 | 是否允许并行工具调用 |
| `previous_response_id` | string | 否 | 必须是当前用户可用的 `resp_*`；不能传 message ID |
| `include` | string[] | 否 | 请求额外字段；仅已实现的上游能力有效 |
| `store` | boolean | 否 | 透传意图不等于本站提供响应历史读取 API |
| `stream` | boolean | 否 | `true` 返回 Responses SSE 事件；必须是 JSON boolean |

数组输入的常见内容块包括 `input_text`、`input_image` 和 `input_file`。图像使用 `image_url`，文件可使用 `file_data` 或 `file_id`，但具体来源、类型与大小限制由目标模型和上游决定。

## 最小请求与非流响应

```bash
curl "$KDAN_BASE_URL/v1/responses" \
  -H "Authorization: Bearer $KDAN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","input":"用一句话解释幂等性。"}'
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
      "text": "幂等性表示同一操作重复执行多次，最终效果与执行一次相同。"
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

不要假定 `output[0]` 是文本。遍历 `output` 并按 `type` 处理 `message`、`reasoning`、`function_call`、`custom_tool_call`、`web_search_call` 等项。仅当 `status` 为 `completed` 时按完整成功处理；`incomplete` 查看 `incomplete_details.reason`，`failed` 查看 `error`。

## SSE 事件与结束处理

流式响应以事件类型驱动。客户端至少应支持：

| 事件 | 用途 |
| --- | --- |
| `response.created` / `response.in_progress` | 初始化响应 ID 与状态 |
| `response.output_item.added` / `.done` | 开始或完成一个类型化输出项 |
| `response.content_part.added` / `.done` | 内容块生命周期 |
| `response.output_text.delta` / `.done` | 累积文本 |
| `response.function_call_arguments.delta` / `.done` | 累积工具参数字符串 |
| `response.reasoning_summary_text.delta` / `.done` | 累积推理摘要（如模型提供） |
| `response.completed` | 成功终止；最终 `response` 可含完整 output 与 usage |
| `response.incomplete` / `response.failed` / `error` | 非成功终止 |

```text
event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_example","output_index":0,"content_index":0,"delta":"幂等性"}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_example","object":"response","created_at":1789401600,"model":"YOUR_MODEL_ID","status":"completed","output":[],"usage":{"input_tokens":18,"output_tokens":24,"total_tokens":42}}}
```

以终止事件中的状态为准。不要只等 `[DONE]`：Responses 的完成语义是 `response.completed`，失败和不完整也有各自事件。连接在终止事件前中断时，将本轮视为未知/可能截断，不要自动执行尚未完成的工具参数。

## 函数工具回合

首轮定义函数。模型返回的 `function_call` 项包含 `call_id`、`name` 和字符串 `arguments`。执行并验证参数后，在下一次请求中回传调用项和结果：

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

HTTP 请求中的 `function_call_output` 必须带 `call_id`。网关不会把缺少调用关联的结果当成合法续轮；仅 Responses WebSocket v2 对 continuation 有不同处理。

## 结构化输出

结构化格式位于 `text.format`，不是 Chat Completions 的 `response_format`：

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

转换链可以映射常见 JSON 格式，但严格约束最终仍由模型执行。客户端必须校验解析结果，并处理拒绝、`incomplete` 与普通文本回退。

## WebSocket 与子路径边界

`GET /v1/responses` 是需要 `Upgrade: websocket` 的入口，不是 Retrieve Response；普通 GET 会返回 426。`POST /v1/responses/*subpath` 只接受网关守卫允许的子路径，不应据此假设官方所有 Responses CRUD 接口均已实现。常规集成优先使用本页的 HTTP/SSE。

## 错误与排障

普通 HTTP 错误形如 `{"error":{"type":"invalid_request_error","message":"..."}}`。常见原因包括 Key 无效、模型缺失、`stream` 类型错误、`previous_response_id` 不是 `resp_*` 或不属于当前用户、模型未开放、额度/并发不足以及上游不可用。

记录 HTTP 状态、request ID、事件类型和最终 `response.status`。不要只记录最后一段文本；对工具工作流还应记录 `call_id`，但应脱敏工具输入。模型列表可见性不代表官方内置工具、存储、子路径或每个 `include` 值均受支持。
