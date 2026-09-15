## Prerequisites

Create an API key in the console and confirm its group. The group controls models, rates, limits, and upstream routing. Do not infer key access from a public model page alone.

Copy the endpoint from the console. `TAPMODELS_BASE_URL` below is the root without a trailing `/v1`; remove that suffix before joining an endpoint when the configured value already contains it.

## Choose a protocol

| Existing client or task | Recommended endpoint |
| --- | --- |
| OpenAI Responses SDK or tool workflow | `POST /v1/responses` |
| OpenAI Chat Completions client | `POST /v1/chat/completions` |
| Anthropic SDK, Claude Code, or Messages payload | `POST /v1/messages` |
| Image generation or editing | `POST /v1/images/generations`, `POST /v1/images/edits` |

## Run and verify

Choose a model enabled for the current group in the examples below. Save the server request ID, read the protocol-specific result, and compare the response model, usage, cache fields, and cost with the console usage record.

When a model returns 404, check the key group, exact model ID, and endpoint scope before retrying. Production clients should set timeouts, handle 429/5xx responses, and keep keys out of logs.
