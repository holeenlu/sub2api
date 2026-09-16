## From an API key to an app

this project exposes compatible OpenAI Responses, Chat Completions, Anthropic Messages, and image APIs. App labels differ, but every integration needs three real values: the API base URL shown by the console, a this project API key, and a model ID enabled for that key's group.

| App | Protocol | Base URL | Authentication |
| --- | --- | --- | --- |
| Codex desktop / CLI | Responses | Console API URL including `/v1` | Generated Legacy, API key, or routed-group environment mode |
| Claude Code | Messages | Console API root without `/v1/messages` | `ANTHROPIC_AUTH_TOKEN` |
| OpenAI SDK | OpenAI compatible | Console API URL including `/v1` | Bearer API key |

Start with the [console configuration builder](/apps/console) to see which client outputs the selected key group supports, then follow the full [Codex](/apps/codex), [Claude Code](/apps/claude-code), or [Claude Desktop](/apps/claude-desktop) guide. Image generation and editing use separate [image skills](/apps/image-skills).

## Create a dedicated key

Open [API keys](/keys) and choose the intended group. This screenshot comes from this project; options follow the installed version.

![Create a this project API key and select its group](/docs-assets/create-api-key.png)

## Recommended setup order

1. Create a key in the console and note its group.
2. Call `GET /v1/models` with that key and copy an exact model ID.
3. Configure the app's base URL, key, and model.
4. Fully quit and restart the app, then create a new test session.
5. Confirm the request, model, and cost in this project usage records.

An empty session list after switching may involve project selection, archives, provider filtering, data directories, or stale index paths. Read [session recovery](/apps/session-recovery) and run the read-only diagnostic before applying a repair. Downloads and checksums are collected under [Downloads](/apps/downloads).
