## From an API key to an app

TapModels exposes compatible OpenAI Responses, Chat Completions, Anthropic Messages, and image APIs. App labels differ, but every integration needs three real values: the API base URL shown by the console, a TapModels API key, and a model ID enabled for that key's group.

| App | Protocol | Base URL | Authentication |
| --- | --- | --- | --- |
| Codex desktop / CLI | Responses | Console API URL including `/v1` | `TAPMODELS_API_KEY` environment variable |
| Claude Code | Messages | Console API root without `/v1/messages` | `ANTHROPIC_AUTH_TOKEN` |
| OpenAI SDK | OpenAI compatible | Console API URL including `/v1` | Bearer API key |


## Create a dedicated key

Open [API keys](/keys) and choose the intended group. This screenshot comes from this project; options follow the installed version.

![Create a TapModels API key and select its group](/docs-assets/create-api-key.png)

## Recommended setup order

1. Create a key in the console and note its group.
2. Call `GET /v1/models` with that key and copy an exact model ID.
3. Configure the app's base URL, key, and model.
4. Fully quit and restart the app, then create a new test session.
5. Confirm the request, model, and cost in TapModels usage records.

Switching an account or provider does not delete local Codex sessions. Older provider identifiers or stale rollout paths can keep existing sessions out of the local index; run the read-only diagnostic before applying a repair.
