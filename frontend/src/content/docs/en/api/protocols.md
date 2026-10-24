## Choose a protocol for your API key's group

Models and permissions depend on the API key's live group. Discover models with the same key and send a minimal request to the target endpoint. A global model catalog or a visible `/v1/models` candidate does not prove that an account is schedulable or that every parameter is available.

| Protocol | Entry point | Gateway boundary |
| --- | --- | --- |
| OpenAI Chat | `POST /v1/chat/completions` | OpenAI, Grok, Kimi, Zhipu, DeepSeek, MiniMax, OpenCodeGo use the OpenAI-compatible handler; other groups may use translation |
| OpenAI Responses | `POST /v1/responses` | Same platform dispatch; guarded `/v1/responses/*subpath` does not expose all official CRUD paths |
| Anthropic Messages | `POST /v1/messages` | Anthropic-compatible groups use the Messages path; the above OpenAI-compatible groups can bridge, without field-by-field equivalence |
| Native Gemini REST | `GET /v1beta/models`, `POST /v1beta/models/{model}:{action}` | Gemini groups; only `generateContent`, `streamGenerateContent`, `countTokens` actions |
| Synchronous/async Images | `/v1/images/generations`, `/edits`, appended `/async`, `GET /v1/images/tasks/{task_id}` | OpenAI/Grok groups only, with image permission and eligible accounts; async needs object storage |
| Embeddings | `POST /v1/embeddings` | OpenAI platform groups only; other platforms return 404 |
| Grok media/search/voice | `/v1/videos...`, `/v1/web_search`, `/v1/x_search`, `/v1/tts`, etc. | Mostly Grok only; selected video create/lookups allow a composite group resolved to Grok |
| Model discovery | `GET /v1/models`, `GET /v1/models/{model}` | Key-scoped candidates; `?client_version=...` produces a different Codex manifest |
| Token count | `/v1/messages/count_tokens`, `/v1/responses/input_tokens`, Gemini `:countTokens` | Can forward or locally estimate, and does not predict final generation usage exactly |

## Authentication and limits

OpenAI-style clients use `Authorization: Bearer $TAPMODELS_API_KEY`; Anthropic SDKs use `x-api-key` plus `anthropic-version`; Gemini SDKs use `x-goog-api-key`. Gemini auth also accepts Bearer, `x-api-key`, and query parameter `key`, but URL keys can leak to proxy logs. Each endpoint applies its relevant deployment body limit and group allowlist, plus billing, concurrency, and content policy where applicable. Tools, images, schema output, caching, and built-in capabilities depend on the actual account and upstream model.

Several `/v1` routes have unprefixed compatibility aliases; new integrations should use `/v1`. `/backend-api/codex/*` and `/antigravity/*` are client/platform-specific. `GET /v1/responses` requires a WebSocket upgrade and is not Retrieve Response; plain GET returns 426.

## Responses and errors

OpenAI-style errors generally use `{"error":{"type":"...","message":"..."}}`, Anthropic uses `{"type":"error","error":{...}}`, and Gemini uses `{"error":{"code":400,"message":"...","status":"INVALID_ARGUMENT"}}`. SSE clients must wait for the protocol's terminal event; an in-stream error or early disconnect is unknown/truncated. Retried generation may duplicate output and tool effects, so applications must design their own idempotency strategy.

This reflects the source gateway, not a production availability guarantee for any dynamic group. Platform dispatch is in `backend/internal/server/routes/gateway.go`; handlers live in `backend/internal/handler/gateway_handler*.go`, `openai_gateway_handler.go`, `gemini_v1beta_handler.go`, `grok_media.go`, and `grok_audio.go`.
