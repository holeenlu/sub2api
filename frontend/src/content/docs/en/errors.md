## Error handling

Tokensavy preserves the inbound protocol's error shape. OpenAI, Anthropic, and Google-style fields can differ. Log the HTTP status, error type, message, and response request ID.

| Status | Common cause | Action |
| --- | --- | --- |
| 400 | Invalid JSON, parameters, model field, or body | Correct the request; do not blindly retry |
| 401 | Missing or invalid API key | Check the header, key state, and base URL |
| 403 | User or group lacks access | Verify key group and authorization |
| 404 | Model not enabled, endpoint unsupported, or conditional feature disabled | Call `/v1/models` and check endpoint scope |
| 413 | Body exceeds the gateway limit | Compress, use a URL, or split the request |
| 429 | Key, group, or upstream rate/concurrency limit | Honor `Retry-After` and use jittered exponential backoff |
| 5xx | Gateway dependency or upstream failure | Keep the request ID and retry only when safe |

Retries can produce a second output and charge. Image and side-effecting tool calls should not be resubmitted unconditionally after a disconnect. Confirm address, authentication, group, model, and endpoint in that order.
