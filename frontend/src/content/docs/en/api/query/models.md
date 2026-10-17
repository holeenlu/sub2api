## Endpoint and authentication

```http
GET /v1/models
Authorization: Bearer $API_KEY
```

The model list depends on the key's group, platform, account/channel mappings, and model allowlist. Query with the same key that will perform inference.

## Minimal request and response

```bash
curl "$API_BASE_URL/v1/models" \
  -H "Authorization: Bearer $API_KEY"
```

OpenAI-compatible groups normally return:

```json
{
  "object":"list",
  "data":[{"id":"YOUR_MODEL_ID","object":"model","created":1704067200,"owned_by":"openai","type":"model","display_name":"YOUR_MODEL_ID"}]
}
```

Anthropic-style items can use `type`, `display_name`, and an ISO `created_at`; Grok items can add reasoning-effort metadata. Treat `data[].id` as the stable discovery field and read optional metadata defensively.

## Visibility is not live availability

The handler gathers account/channel mappings and applies the group's allowlist. Some platforms fall back to code-defined defaults when live mappings are empty; composite groups can also use default candidates. A listed ID therefore means visible integration candidate, not proof of a schedulable account or support for every Chat, Responses, Messages, or Images parameter.

List, choose an exact ID, send the target endpoint's minimal request, then confirm the HTTP response and usage. Refresh a short-lived cache immediately after `model_not_found` or a no-account error.

## Single-model path and Codex mode

`GET /v1/models/{model}` reuses the Models handler and filters by the path parameter; do not assume every platform emits the official Retrieve Model field set.

Native Gemini SDKs use separate `GET /v1beta/models` and `GET /v1beta/models/{model}` routes with `models[]` / `models/...` shapes; the ordinary path is restricted to Gemini groups. Do not impose the OpenAI-style `/v1/models` envelope on native Gemini discovery.

Adding `client_version` to `GET /v1/models` selects a Codex model-manifest path rather than ordinary `{"object":"list","data":[]}` output. General SDKs and application model pickers should omit it.

## Errors and troubleshooting

401 generally means a missing/invalid key; 403/404 can reflect group or allowlist policy; 5xx indicates a gateway dependency or upstream discovery failure. Record status, request ID, and error body, and confirm that key and base URL match the inference request. A global model marketplace is not the same as this key-scoped list.
