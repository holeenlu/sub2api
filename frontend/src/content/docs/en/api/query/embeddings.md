## Endpoint and platform

```http
POST /v1/embeddings
Authorization: Bearer $KDAN_API_KEY
Content-Type: application/json
```

The gateway route allows OpenAI platform groups only. Other groups receive 404 `not_found_error` with `Embeddings API is not supported for this platform`. The model must also be enabled for the group, with an eligible embeddings-capable account and billing capacity. A visible ID from `/v1/models` is not proof that it is an embedding model.

## Request and response

```bash
curl "$KDAN_BASE_URL/v1/embeddings" \
  -H "Authorization: Bearer $KDAN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_EMBEDDING_MODEL_ID","input":"Idempotency means repeatable effects."}'
```

`model` is a required non-empty string. `input` accepts text or arrays as permitted by the target OpenAI-compatible embedding model. Other fields such as `encoding_format` and `dimensions` depend on upstream support; the gateway does not guarantee them on every account. First validate one text input using the exact ID.

An illustrative response (not a real vector dimension):

```json
{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.012,-0.008]}],"model":"YOUR_EMBEDDING_MODEL_ID","usage":{"prompt_tokens":8,"total_tokens":8}}
```

Align batch inputs by `index` and validate the vector dimension and finite values. This is a synchronous JSON endpoint, not Chat SSE.

## Errors and implementation

Empty/invalid JSON or missing `model` returns 400 `invalid_request_error`; an invalid key returns 401; a mismatched platform returns 404. No eligible embeddings account or upstream failure can produce scheduling/upstream errors. The handler applies model mapping, content policy, concurrency, and billing eligibility, then selects an account with Embeddings endpoint capability.

The platform restriction is in `backend/internal/server/routes/gateway.go`; validation/selection in `backend/internal/handler/openai_embeddings.go`; forwarding in `backend/internal/service/openai_embeddings.go`. This page does not claim production group or vector-response validation.
