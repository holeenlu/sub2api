## Three token-counting endpoints

| Protocol | Request | Success fields | Semantics |
| --- | --- | --- | --- |
| Anthropic | `POST /v1/messages/count_tokens` | `input_tokens` | Anthropic paths can forward; OpenAI paths bridge; Grok and some compatible providers estimate locally |
| Responses | `POST /v1/responses/input_tokens` | `object: response.input_tokens`, `input_tokens` | Compatible official accounts can forward; unsupported and selected compatible accounts fall back locally |
| Gemini | `POST /v1beta/models/{model}:countTokens` | `totalTokens` | Gemini groups only; the Antigravity OAuth account path currently returns placeholder `0` |

A count is not a final bill prediction. Protocol wrappers, tool schemas, images, caching, reasoning tokens, tokenizer choice, and model mapping can change actual generation usage.

## Anthropic Messages count

```bash
curl "$TOKENSAVY_BASE_URL/v1/messages/count_tokens" \
  -H "x-api-key: $TOKENSAVY_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","system":"Answer briefly.","messages":[{"role":"user","content":"What is idempotency?"}]}'
```

```json
{"input_tokens":19}
```

The body follows Messages input and may include `system`, `messages`, `tools`, and thinking fields. The Grok path selects no account and makes no upstream call: it converts the protocol and estimates with a local tokenizer, so use it for capacity planning. An Anthropic-compatible upstream that explicitly lacks counting can return 404.

## Responses input_tokens

```bash
curl "$TOKENSAVY_BASE_URL/v1/responses/input_tokens" \
  -H "Authorization: Bearer $TOKENSAVY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","instructions":"Answer briefly.","input":"What is idempotency?"}'
```

```json
{"object":"response.input_tokens","input_tokens":19}
```

`model` is a required non-empty string. Other fields follow Responses input, including string or typed `input`, `instructions`, and tools. A local fallback also returns HTTP 200 and the same envelope, so the shape does not prove an exact upstream count. Use actual generation usage for billing.

## Gemini countTokens

```bash
curl "$TOKENSAVY_BASE_URL/v1beta/models/YOUR_MODEL_ID:countTokens" \
  -H "x-goog-api-key: $TOKENSAVY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"role":"user","parts":[{"text":"What is idempotency?"}]}]}'
```

```json
{"totalTokens":19}
```

The number above only illustrates the response shape. The ordinary `/v1beta` path is restricted to Gemini groups. If routing selects an Antigravity OAuth account, the implementation always returns `{"totalTokens":0}`. Do not use this placeholder for capacity planning or precise cost estimates. Query `/v1beta/models` with the same key and remove the `models/` prefix from its `name` before placing the ID in the URL.

## Errors and implementation boundary

Missing keys, empty bodies, missing models, allowlist failures, and unavailable accounts return protocol-specific errors. Counting creates no generation usage record, but authentication, group policy, content review, billing eligibility, concurrency where applicable, and body limits still apply. See `backend/internal/server/routes/gateway.go`, `handler/openai_gateway_count_tokens.go`, `service/openai_gateway_count_tokens.go`, `service/gateway_count_tokens.go`, and `handler/gemini_v1beta_handler.go`.
