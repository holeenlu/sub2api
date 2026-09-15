## Endpoint and authentication

```http
POST /v1/chat/completions
Authorization: Bearer $TAPMODELS_API_KEY
Content-Type: application/json
```

This endpoint accepts OpenAI Chat Completions requests. The model must be visible to the API key's group. The gateway can forward natively or translate among Chat Completions, Responses, Messages, and other compatible protocols, so exact model support must be confirmed with `GET /v1/models` and a minimal request.

## Core request fields

| Field | Type | Required | Constraints and behavior |
| --- | --- | --- | --- |
| `model` | string | yes | Exact model ID; an empty value returns `invalid_request_error` |
| `messages` | array | yes | Ordered `system`, `developer`, `user`, `assistant`, and `tool` messages |
| `messages[].content` | string / array / null | role-dependent | Text or multimodal parts; an assistant tool-call message may use `null` |
| `max_completion_tokens` | integer | no | Preferred output limit; older clients may use `max_tokens`. Model limits apply |
| `temperature` / `top_p` | number | no | Accepted ranges and whether reasoning models ignore them are model-dependent |
| `stream` | boolean | no | Returns `text/event-stream`; must be a JSON boolean |
| `stream_options.include_usage` | boolean | no | Requests a final usage chunk. The gateway may force it upstream for billing, but clients must tolerate missing usage |
| `tools` | array | no | Functions use `type: "function"` plus `function.{name,description,parameters}` |
| `tool_choice` | string / object | no | Common values are `auto`, `none`, `required`, or a named function; path support varies |
| `parallel_tool_calls` | boolean | no | Allows multiple calls; translation can narrow the semantics |
| `reasoning_effort` | string | no | The gateway parses `low`, `medium`, `high`, and `xhigh`; not every model supports every value |
| `stop` | string / string[] | no | Sequence count and length limits come from the upstream |
| `response_format` | object | no | Structured output for models that support `json_object` or `json_schema` |

User messages can contain `text` and `image_url` parts. Compatibility types also accept file parts, but URL/data-URI sources, file types, and sizes remain processing-path constraints.

## Minimal request

```bash
curl "$TAPMODELS_BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $TAPMODELS_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","messages":[{"role":"user","content":"Explain idempotency in one sentence."}]}'
```

## Complete non-streaming response

```json
{
  "id":"chatcmpl_example",
  "object":"chat.completion",
  "created":1789401600,
  "model":"YOUR_MODEL_ID",
  "choices":[{"index":0,"message":{"role":"assistant","content":"Idempotency means repeating an operation has the same final effect as performing it once."},"finish_reason":"stop"}],
  "usage":{"prompt_tokens":18,"completion_tokens":24,"total_tokens":42,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}
}
```

Read `choices[].message` and branch on `finish_reason`: `stop` is a natural end, `length` hit the output limit, `tool_calls` requires tool execution, and `content_filter` indicates filtering. Provider-specific fields may be lost during translation.

## SSE streaming

Each frame is `data: {JSON}\n\n`. Text is in `choices[].delta.content`. Tool names and `function.arguments` can be split across frames; accumulate by choice and tool index and parse arguments only after the call finishes.

```text
data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[{"index":0,"delta":{"content":"Idempotency"},"finish_reason":null}]}

data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[],"usage":{"prompt_tokens":18,"completion_tokens":24,"total_tokens":42}}

data: [DONE]
```

Treat `[DONE]`, a final `finish_reason`, or a usage tail frame as successful completion evidence. If the connection closes before all three, mark the result as possibly truncated and retry only with an application-level idempotency policy. After HTTP streaming starts, failures may arrive in-stream or as a disconnect.

## Tool-call round trip

Define functions in the first request. When the assistant returns `tool_calls`, execute and validate each call, then send the original assistant item and matching results:

```json
{
  "model":"YOUR_MODEL_ID",
  "messages":[
    {"role":"user","content":"What time is it in Shanghai?"},
    {"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_time","arguments":"{\"timezone\":\"Asia/Shanghai\"}"}}]},
    {"role":"tool","tool_call_id":"call_1","content":"{\"time\":\"10:30\"}"}
  ],
  "tools":[{"type":"function","function":{"name":"get_time","description":"Return local time for an IANA timezone","parameters":{"type":"object","properties":{"timezone":{"type":"string"}},"required":["timezone"],"additionalProperties":false}}}]
}
```

`function.arguments` is a string. Parse it as JSON and enforce application permissions before execution. Every result's `tool_call_id` must match the original call.

## Structured output

For a compatible model, send:

```json
"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}}}
```

The gateway maps common Chat `response_format` and Responses `text.format` shapes on some translation paths, but cannot guarantee strict-schema enforcement by every provider. Parse and validate the output and handle refusal, truncation, or plain-text fallback.

## Errors and troubleshooting

Normal errors use `{"error":{"type":"...","message":"..."}}`. Typical cases are 401 invalid key; 400 invalid body, model, or stream type; 403 disabled group capability; 404 unavailable model; 429 quota or concurrency; and 502/503 upstream or scheduling failure.

Record the request ID header, HTTP status, and full `error.type/message`, while excluding keys, sensitive tool arguments, and large data URIs. Query models with the same key, then reduce to the minimal request above. Listing a model does not prove support for every Chat parameter.
