## Endpoint and authentication

```http
POST /v1/images/generations
POST /v1/images/edits
Authorization: Bearer $KDAN_API_KEY
```

The currently showcased image models include `gpt-image-2.5-flare` and `gpt-image-2.5-sunburst`; access is determined by the API key's group and available compatible accounts. The gateway validates the `gpt-image-*` family. If `model` is omitted, code defaults to `gpt-image-2`, but that default is not proof of schedulability, so production clients should send an exact ID returned by `GET /v1/models`.

## Generation request

Generation uses JSON:

```bash
curl "$KDAN_BASE_URL/v1/images/generations" \
  -H "Authorization: Bearer $KDAN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-image-2.5-flare","prompt":"A clean product photo of a red desk lamp on a white background","n":1,"response_format":"b64_json"}'
```

| Field | Type | Required | Gateway behavior and limits |
| --- | --- | --- | --- |
| `model` | string | recommended | Must be a GPT image family or implemented Grok image model; omitted value defaults to `gpt-image-2` |
| `prompt` | string | yes | Generation/edit instruction; an empty prompt is rejected by the target path |
| `n` | integer | no | Defaults to 1; gateway requires greater than 0; upstream sets the maximum |
| `size` | string | no | Upstream size or tier; billing normalization recognizes 1K/2K/4K, while actual pixels come from the file |
| `quality` | string | no | Native option; values depend on the exact model |
| `response_format` | string | no | `b64_json` or an upstream-supported `url`; URLs can expire |
| `background` / `output_format` | string | no | Native options; require model support |
| `output_compression` | integer | no | Type is validated by the gateway; range is upstream-defined |
| `moderation` / `style` | string | no | Native options; not supported by every model |
| `partial_images` | integer | no | Number of partial images for native streaming |
| `stream` | boolean | no | Image SSE when true; must be a boolean |

Do not infer quality, size, format, or multi-image limits from another GPT Image version. Explicit model/size, streaming, `n != 1`, masks, and native options require an account with `images-native` capability.

## Edits: multipart/form-data

The edit endpoint accepts one or more `image` / `image[n]` parts and an optional `mask`. Each uploaded part is read up to 20 MiB; the total request is also bounded by gateway configuration.

```bash
curl "$KDAN_BASE_URL/v1/images/edits" \
  -H "Authorization: Bearer $KDAN_API_KEY" \
  -F "model=gpt-image-2.5-flare" \
  -F "prompt=Replace the background with a quiet library" \
  -F "image=@input.png;type=image/png" \
  -F "mask=@mask.png;type=image/png" \
  -F "response_format=b64_json"
```

Multipart requires a boundary. An edit without an image returns `image file is required`. `mask` is uploaded as a separate image; transparency semantics, matching dimensions, and accepted formats are validated by the selected model. The gateway does not derive dimensions from the part header alone.

JSON edits are also accepted as `"images":[{"image_url":"https://..."}]`, with a mask using `{"image_url":"..."}`. At least one `images[].image_url` is required; `images[].file_id` and `mask.file_id` are explicitly rejected.

## Non-streaming response

```json
{"created":1789401600,"data":[{"b64_json":"iVBORw0KGgoAAA...","revised_prompt":"A clean product photo of a red desk lamp on white."}],"usage":{"input_tokens":120,"output_tokens":1056,"total_tokens":1176}}
```

Results can instead contain `data[].url`. Decode only the structured `b64_json` field, validate base64, MIME, and real dimensions, and keep large payloads out of logs. Treat URLs as short-lived results and move them to controlled storage promptly.

## Image SSE

Native streams emit `image_generation.partial_image` and `image_generation.completed` (edit paths can normalize names to `image_edit.*`). Payloads can include `b64_json`, `partial_image_index`, `size`, `output_format`, and usage. Other accounts can expose Responses events, so clients should branch on JSON `type` and tolerate `response.*` and `response.image_generation_call.*` events.

Only a completed image event or a final response containing a valid image result is success. `response.incomplete`, `response.failed`, `error`, empty output, or a disconnect before completion is failure/unknown even when HTTP status is 200.

## Async submit and poll

Async tasks require administrator-enabled object storage. When disabled, submit returns 404, while already-created tasks remain pollable if the task store is available. Async requests use the same JSON or multipart payload as synchronous endpoints and reject `stream: true`.

```http
POST /v1/images/generations/async
POST /v1/images/edits/async
GET  /v1/images/tasks/{task_id}
```

An accepted submit returns HTTP 202 with `Location: /v1/images/tasks/{task_id}`, `Retry-After: 3`, and:

```json
{"id":"imgtask_abc123","task_id":"imgtask_abc123","object":"image.generation.task","status":"processing","created_at":1789401600,"expires_at":1789488000,"poll_url":"/v1/images/tasks/imgtask_abc123"}
```

Poll with the same API key. Actual statuses are only `processing`, `completed`, and `failed`:

```json
{"id":"imgtask_abc123","task_id":"imgtask_abc123","object":"image.generation.task","status":"completed","http_status":200,"image_url":"https://storage.example/result.png","result":{"created":1789401601,"data":[{"url":"https://storage.example/result.png"}]},"created_at":1789401600,"completed_at":1789401601,"expires_at":1789488001}
```

Processing responses include `Retry-After: 3`; failed tasks return the synchronous HTTP status and `error`. Ownership is checked by user and API key, and another key receives 404. Default result TTL is 24 hours and execution timeout is 30 minutes; deployment settings can override both. No webhook is provided by this interface.

## Errors and troubleshooting

Errors use `{"error":{"type":"...","message":"..."}}`; async responses also expose `error.code`. Check image-family model, group permission, multipart boundary and field names, `n`/`stream`/compression types, object-storage enablement, and the original key for polling. Preserve request ID, model, endpoint, status, and last event type for 502/503, empty output, or stream errors while excluding keys, source/mask data, and generated payloads.

## Batch image jobs (separate API)

```http
POST   /v1/images/batches
GET    /v1/images/batches
GET    /v1/images/batches/models
GET    /v1/images/batches/{id}
GET    /v1/images/batches/{id}/items
GET    /v1/images/batches/{id}/items/{custom_id}/content
POST   /v1/images/batches/{id}/cancel
DELETE /v1/images/batches/{id}
DELETE /v1/images/batches/{id}/outputs
```

Submit JSON with at least a `model` and non-empty `items`. Each item may include `custom_id`, `prompt`, `output_count`, and `reference_images`; the batch also accepts `task_name`, `parent_batch_id`, `provider`, `response_mime_type`, `aspect_ratio`, `image_size`, and `metadata`. Defaults are 200 items, up to 4 outputs per item, 10 MiB per reference image, and 1,000 references/128 MiB per batch; deployment settings can change them. The response includes `id`, `status`, `item_count`, `estimated_cost`, and `hold_amount`; use the same key to query and send `Idempotency-Key` for submission deduplication. This is a gateway batch capability, not the official OpenAI Batch API.
