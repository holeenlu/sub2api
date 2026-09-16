## Endpoint and authentication

```http
POST /v1/messages
x-api-key: $API_KEY
anthropic-version: 2023-06-01
Content-Type: application/json
```

Bearer authentication is also accepted, but Anthropic SDK and Claude Code integrations should retain `x-api-key`. Anthropic groups normally use a Messages-compatible path; OpenAI, Grok, Kimi, Zhipu, DeepSeek, MiniMax, and OpenCodeGo groups can bridge through the OpenAI gateway. Blocks, errors, stop reasons, and usage are not guaranteed to remain identical across protocols.

Estimate input tokens with `POST /v1/messages/count_tokens`. It accepts this page's Messages body and returns `{"input_tokens":...}`. Grok and selected OpenAI-compatible paths can use a local tokenizer, so this value is not final usage or billing.

## Core request fields

| Field | Type | Required | Constraints and behavior |
| --- | --- | --- | --- |
| `model` | string | yes | Exact model ID enabled for the group |
| `max_tokens` | integer | yes | Maximum output tokens, bounded by model/group limits |
| `messages` | array | yes | `user` and `assistant` sequence; put system instructions in top-level `system` |
| `messages[].content` | string / array | yes | Text and implemented image, thinking, tool-use, and tool-result blocks |
| `system` | string / array | no | Top-level instructions; array blocks can use supported `cache_control` |
| `temperature` / `top_p` | number | no | Range and support are model-dependent |
| `stop_sequences` | string[] | no | Custom stop sequences |
| `thinking` | object | no | Parsed types are `enabled`, `adaptive`, and `disabled`; enabled may set `budget_tokens` |
| `output_config.effort` | string | no | Parsed as `low/medium/high/max`; model support applies |
| `tools` | array | no | Usually contains `name`, `description`, and `input_schema` |
| `tool_choice` | object | no | Automatic, any, or named tool; compatibility varies |
| `stream` | boolean | no | Emits Anthropic-style SSE |

An image block uses `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"..."}}`. Cache control parses `{"type":"ephemeral","ttl":"5m"}` or `1h`; cache eligibility, minimum prefix, and pricing remain model-specific.

## Minimal request and complete response

```bash
curl "$API_BASE_URL/v1/messages" \
  -H "x-api-key: $API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","max_tokens":256,"messages":[{"role":"user","content":"Explain idempotency in one sentence."}]}'
```

```json
{
  "id":"msg_example","type":"message","role":"assistant",
  "content":[{"type":"text","text":"Idempotency means repeating an operation has the same final effect as performing it once."}],
  "model":"YOUR_MODEL_ID","stop_reason":"end_turn","stop_sequence":null,
  "usage":{"input_tokens":18,"output_tokens":24,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}
}
```

Iterate over all `content` blocks. Common stop reasons are `end_turn`, `max_tokens`, `stop_sequence`, and `tool_use`; translations can change the exact value.

## SSE events and termination

A normal sequence is `message_start`, one or more `content_block_start` / `content_block_delta` / `content_block_stop` lifecycles, then `message_delta` and `message_stop`. Delta types include `text_delta`, `thinking_delta`, `signature_delta`, and tool `input_json_delta`.

```text
event: message_start
data: {"type":"message_start","message":{"id":"msg_example","type":"message","role":"assistant","content":[],"model":"YOUR_MODEL_ID","stop_reason":null,"usage":{"input_tokens":18,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Idempotency"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":24}}

event: message_stop
data: {"type":"message_stop"}
```

Accumulate by `index`. `input_json_delta.partial_json` is only a fragment; parse after its `content_block_stop`. Treat `message_stop` as normal completion. A disconnect or `event: error` before that leaves text, thinking, or arguments incomplete.

## Tool-call round trip

```json
{
  "model":"YOUR_MODEL_ID","max_tokens":256,
  "messages":[
    {"role":"user","content":"What time is it in Shanghai?"},
    {"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"get_time","input":{"timezone":"Asia/Shanghai"}}]},
    {"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"{\"time\":\"10:30\"}"}]}
  ],
  "tools":[{"name":"get_time","description":"Return local time for an IANA timezone","input_schema":{"type":"object","properties":{"timezone":{"type":"string"}},"required":["timezone"],"additionalProperties":false}}]
}
```

`tool_use_id` must match. Validate model-generated input and application permissions before execution; tool failures can be returned with `is_error: true`.

## Structured output and compatibility

The core Messages mechanism for structured arguments is tool `input_schema`. Use a native structured-output option only when the exact model documents it; do not copy Chat `response_format` or Responses `text.format` into this endpoint. Protocol bridges attempt to preserve functions, reasoning, and stop reasons, but provider server tools, caching semantics, and encrypted reasoning round trips require target-group testing.

## Errors and troubleshooting

Messages errors use `{"type":"error","error":{"type":"invalid_request_error","message":"..."}}`. Check HTTP status, request ID, type, and message: 401 is usually a key issue; 400 a body/model/block issue; 403/404 group or model policy; 429 quota/concurrency; 502/503 upstream capacity. Query models with the same key and reduce to the minimal request. For streams, retain the last complete event type and block index, excluding keys, base64 images, thinking, and sensitive tool input.
