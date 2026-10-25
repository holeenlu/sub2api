## Base URL

The real KDAN endpoint comes from public system settings and the console. Raw HTTP requests use `{root}/v1/...`. OpenAI SDKs normally use `{root}/v1` as `base_url`; Anthropic SDKs use the root and append `/v1/messages`.

The examples normalize configured values to avoid `/v1/v1`. Custom deployment domains, proxy paths, and runtime settings take precedence over placeholders.

## Recommended headers

| Protocol | Header |
| --- | --- |
| OpenAI compatible | `Authorization: Bearer $KDAN_API_KEY` |
| Anthropic Messages | `x-api-key: $KDAN_API_KEY` |
| Gemini compatible | `x-goog-api-key: $KDAN_API_KEY` |

Use one key header per request. The gateway reads a valid Bearer header before the two compatibility headers. Raw Messages requests also include the applicable `anthropic-version`.

## Keys, groups, and safety

A key's group controls model allowlists, routing, rates, concurrency, and other policy. The public plaza is not a guarantee for every key; use `GET /v1/models` as discovery and verify with an actual request.

Keep keys in server-side environment variables or a secret manager. Never put them in URLs, browser code, screenshots, repositories, or logs. Revoke and replace a leaked key immediately.
