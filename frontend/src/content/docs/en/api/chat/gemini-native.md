## Supported native endpoints

```http
GET  /v1beta/models
GET  /v1beta/models/{model}
POST /v1beta/models/{model}:generateContent
POST /v1beta/models/{model}:streamGenerateContent?alt=sse
POST /v1beta/models/{model}:countTokens
x-goog-api-key: $KDAN_API_KEY
```

The ordinary `/v1beta` surface accepts Gemini groups only. A separate `/antigravity/v1beta` prefix forces the Antigravity platform and does not imply that an ordinary Gemini group has the same accounts or models.

The handler allows only `generateContent`, `streamGenerateContent`, and `countTokens`. `embedContent`, batch embedding, cached contents, files, and tuning are not implemented on this gateway surface; the action is rejected even if the wildcard route matches its URL.

## Model discovery

```bash
curl "$KDAN_BASE_URL/v1beta/models" -H "x-goog-api-key: $KDAN_API_KEY"
```

The response uses Gemini's `models[]` envelope; `name` is normally `models/YOUR_MODEL_ID`. Items can come from the upstream or a code-defined fallback when selected account types cannot discover models, then pass through the group allowlist. Visibility is not proof of live scheduling.

## generateContent request

```bash
curl "$KDAN_BASE_URL/v1beta/models/YOUR_MODEL_ID:generateContent" \
  -H "x-goog-api-key: $KDAN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "systemInstruction":{"parts":[{"text":"Answer briefly."}]},
    "contents":[{"role":"user","parts":[{"text":"Explain idempotency."}]}],
    "generationConfig":{"temperature":0.2,"maxOutputTokens":256}
  }'
```

| Field | Required | Notes |
| --- | --- | --- |
| URL `{model}` | yes | An ID allowed for this key; path characters are restricted to letters, digits, `_`, `-`, `.` |
| `contents` | yes | Conversation array with `role` and non-empty `parts` |
| `parts[].text` | task-dependent | Text; multimodal `inlineData`/`fileData` requires account/model support |
| `systemInstruction` | no | Gemini top-level system instruction |
| `generationConfig` | no | Temperature, output limit, stop sequences, response MIME/schema, subject to the target |
| `tools` / `toolConfig` | no | Function declarations/selection; return results as a `functionResponse` part |
| `safetySettings` | no | Accepted values depend on the account type and upstream model |

The gateway removes messages with empty `parts` and may add a function-call thought signature for strict upstreams; clients should still send canonical input. Iterate `candidates[]` and `content.parts`, then inspect `finishReason`, `usageMetadata`, and possible `promptFeedback`. Empty candidates or an abnormal finish reason are not a complete success.

## SSE streaming

```bash
curl -N "$KDAN_BASE_URL/v1beta/models/YOUR_MODEL_ID:streamGenerateContent?alt=sse" \
  -H "x-goog-api-key: $KDAN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"role":"user","parts":[{"text":"Explain idempotency."}]}]}'
```

The URL action selects streaming; there is no body `stream` switch. Each `data:` is a Gemini response fragment. Accumulate by candidate index and part, then read finish reason and usage from the final fragment. A disconnect before a complete terminal fragment is possibly truncated. Some OAuth/Code Assist accounts internally stream and aggregate even for non-streaming input, so downstream format does not reveal upstream transport.

## Count, errors, and limits

`:countTokens` uses a `contents` body and returns `totalTokens`. The Antigravity OAuth account path currently returns placeholder `0` without counting input; do not use it for capacity planning or precise estimates. Errors use Google's envelope: `{"error":{"code":400,"message":"...","status":"INVALID_ARGUMENT"}}`. Common causes include key/group mismatch, unsafe model paths, unsupported action, empty body, allowlist policy, quota/concurrency, or upstream failure.

See `backend/internal/server/routes/gateway.go`, `handler/gemini_v1beta_handler.go`, `service/gemini_messages_compat_service.go`, `service/antigravity_gateway_gemini.go`, and `service/vertex_service_account.go`.
