## Grok-specific surface

Grok text uses the OpenAI-compatible Chat, Responses, and Messages bridges. This page covers Grok-only media, search, and voice routes.

| Capability | Endpoint | Boundary |
| --- | --- | --- |
| Images | `POST /v1/images/generations`, `/edits` | Grok groups, with image permission and eligible media accounts |
| Async images | Append `/async`, poll `GET /v1/images/tasks/{task_id}` | OpenAI/Grok groups with object storage enabled |
| Video | `/v1/videos`, `/videos/generations`, `/edits`, `/extensions` | Create/lookups may allow a composite group resolved to Grok; edits/extensions require Grok |
| Search | `POST /v1/web_search`, `/v1/x_search` | Grok groups only |
| HTTP voice | `/v1/tts`, `/v1/stt`, `/v1/custom-voices...` | Grok groups only; native body/response relay |
| Realtime voice | `GET /v1/realtime?model=...` | Grok only; WebSocket Upgrade required |

Use a Bearer key for HTTP routes. Model visibility from `/v1/models` does not prove media eligibility.

## Images and video

Grok images reuse OpenAI Images paths. JSON or multipart may carry `model`, `prompt`, `n`, `size`, `aspect_ratio`, `resolution`, and input images; edits accept at most three source images. Exact size, ratio, count, and mask semantics come from the upstream model.

Video routes are:

```http
POST /v1/videos
POST /v1/videos/generations
POST /v1/videos/edits
POST /v1/videos/extensions
GET  /v1/videos/{request_id}
GET  /v1/videos/{request_id}/content
```

Creation usually needs a model and prompt, with optional `resolution`, `duration`, `aspect_ratio`, and input-image URLs. Poll the returned request ID and aliases. Code treats a result as complete only when `status: "done"` and `video.url` are present; content is proxied through the account bound to the task.

## Standalone search

```bash
curl "$TAPMODELS_BASE_URL/v1/web_search" \
  -H "Authorization: Bearer $TAPMODELS_API_KEY" -H "Content-Type: application/json" \
  -d '{"query":"TapModels API updates","max_results":5}'
```

`query` is required and `input` is accepted as an alias. `max_results` defaults to 5 and is capped at 20. `/v1/x_search` also accepts `allowed_x_handles`, `excluded_x_handles`, `from_date`, `to_date`, `enable_image_understanding`, and `enable_video_understanding`. The response is a gateway aggregate (`query/results/provider/max_results`), not raw Responses tool events; validate and deduplicate URLs.

## Voice and Realtime

TTS, STT, and custom voices preserve the client Content-Type and relay the native body/response; fields and media types are upstream-defined. Custom voices support listing, creation, retrieval, update, deletion, and audio retrieval. Realtime defaults `model` to `grok-voice-latest` and requires an Upgrade; plain GET returns 426.

Non-Grok groups generally receive 404 `not_found_error` on these routes. Other failures include empty bodies, capability mismatch, no account, quota/concurrency, and upstream 4xx/5xx. Source references: `routes/gateway.go`, `handler/grok_media.go`, `service/grok_media.go`, `handler/gateway_web_search.go`, `handler/openai_x_search.go`, and `handler/grok_audio.go`.
