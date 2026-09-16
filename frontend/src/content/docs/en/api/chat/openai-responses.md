## Endpoint and authentication

```http
POST /v1/responses
Authorization: Bearer $KDAN_API_KEY
Content-Type: application/json
```

This is the OpenAI Responses-style endpoint. The key's group selects a native or translated path. Built-in tools present in the official API are not automatically enabled here.

Use `POST /v1/responses/input_tokens` for input-only counting. It requires a non-empty `model` and returns `{"object":"response.input_tokens","input_tokens":...}`. Some account paths estimate locally, so it does not replace actual generation usage.

## Core request fields

| Field | Type | Required | Constraints and behavior |
| --- | --- | --- | --- |
| `model` | string | yes | Exact model ID enabled for the group |
| `input` | string / array | yes | Plain text or typed messages, function calls, and function results |
| `instructions` | string | no | Top-level instructions for this response |
| `max_output_tokens` | integer | no | Translation code uses a safety floor of 128; lower values may be raised or rejected |
| `temperature` / `top_p` | number | no | Effect and range are model-dependent |
| `reasoning` | object | no | Parsed effort: `low/medium/high/xhigh`; summary: `auto/concise/detailed`; model support still applies |
| `text` | object | no | `format` configures structured output; parsed verbosity is `low/medium/high` |
| `tools` | array | no | Types include function, custom, and several client/search tools; availability is path-specific |
| `tool_choice` | string / object | no | Automatic, disabled, required, or named tool; translation can narrow it |
| `parallel_tool_calls` | boolean | no | Allows parallel calls |
| `previous_response_id` | string | no | Must be an accessible `resp_*`, not a message ID |
| `include` | string[] | no | Requests extra fields supported by the selected upstream path |
| `store` | boolean | no | A passthrough intent; it does not create a KDAN retrieve-history API |
| `stream` | boolean | no | Emits Responses SSE events; must be a JSON boolean |

Common array parts include `input_text`, `input_image`, and `input_file`. Image URLs and file data/IDs remain subject to upstream type and size restrictions.

## Minimal request and complete response

```bash
curl "$KDAN_BASE_URL/v1/responses" \
  -H "Authorization: Bearer $KDAN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","input":"Explain idempotency in one sentence."}'
```

```json
{
  "id":"resp_example","object":"response","created_at":1789401600,"model":"YOUR_MODEL_ID","status":"completed",
  "output":[{"id":"msg_example","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Idempotency means repeating an operation has the same final effect as performing it once."}]}],
  "usage":{"input_tokens":18,"output_tokens":24,"total_tokens":42,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}
}
```

Iterate over `output` by `type`; do not assume `output[0]` is text. Handled shapes include `message`, `reasoning`, `function_call`, `custom_tool_call`, and `web_search_call`. Only `completed` is a full success. For `incomplete`, inspect `incomplete_details.reason`; for `failed`, inspect `error`.

## SSE events and termination

| Event | Purpose |
| --- | --- |
| `response.created` / `response.in_progress` | Initialize response ID and state |
| `response.output_item.added` / `.done` | Typed output-item lifecycle |
| `response.content_part.added` / `.done` | Content-part lifecycle |
| `response.output_text.delta` / `.done` | Accumulate text |
| `response.function_call_arguments.delta` / `.done` | Accumulate argument strings |
| `response.reasoning_summary_text.delta` / `.done` | Accumulate a reasoning summary when present |
| `response.completed` | Successful terminal event with final response/usage |
| `response.incomplete` / `response.failed` / `error` | Non-success terminal events |

```text
event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_example","output_index":0,"content_index":0,"delta":"Idempotency"}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_example","object":"response","created_at":1789401600,"model":"YOUR_MODEL_ID","status":"completed","output":[],"usage":{"input_tokens":18,"output_tokens":24,"total_tokens":42}}}
```

Use the terminal event's state. Responses completion is `response.completed`, not merely `[DONE]`. A disconnect before a terminal event is unknown/possibly truncated; never execute unfinished tool arguments.

## Function-tool round trip

```json
{
  "model":"YOUR_MODEL_ID",
  "input":[
    {"type":"function_call","call_id":"call_1","name":"get_time","arguments":"{\"timezone\":\"Asia/Shanghai\"}"},
    {"type":"function_call_output","call_id":"call_1","output":"{\"time\":\"10:30\"}"}
  ],
  "tools":[{"type":"function","name":"get_time","description":"Return local time for an IANA timezone","parameters":{"type":"object","properties":{"timezone":{"type":"string"}},"required":["timezone"],"additionalProperties":false},"strict":true}]
}
```

An HTTP `function_call_output` requires `call_id`. The gateway rejects an unassociated result; continuation by `previous_response_id` has different handling only on Responses WebSocket v2.

## Structured output

Responses uses `text.format`, rather than Chat Completions `response_format`:

```json
"text":{"format":{"type":"json_schema","name":"answer","strict":true,"schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}}}
```

Translation can map common JSON formats, but strict enforcement remains a model behavior. Validate parsed output and handle refusal, `incomplete`, or plain-text fallback.

## WebSocket and subpath boundary

`GET /v1/responses` requires `Upgrade: websocket`; it is not Retrieve Response, and a normal GET returns 426. Guarded `POST /v1/responses/*subpath` routes do not imply that all official CRUD endpoints exist. Prefer HTTP/SSE for general integrations.

## Errors and troubleshooting

HTTP errors use `{"error":{"type":"invalid_request_error","message":"..."}}`. Common causes include invalid keys, missing model, invalid stream type, an invalid or inaccessible `previous_response_id`, model policy, quota/concurrency, and upstream failure. Record HTTP status, request ID, event type, final status, and redacted `call_id` context. Model visibility does not prove support for every built-in tool, storage feature, subpath, or `include` value.
