## Console configuration builder

**API keys → Use key** is the app configuration builder. It renders copyable shell commands, config files, and a Codex model catalog for OpenAI/Composite groups. Treat the values in the current modal as authoritative for the address, environment variable names, and model examples.

## Create a key and choose its group

1. Open [API keys](/keys) and create a dedicated key.
2. Select the group that should receive the requests. The group controls model visibility, protocol routing, balance, and limits; a copied key without its group often results in 401 or unknown-model errors.
3. Click **Use key** and choose one client tab. Do not paste one generated snippet into multiple clients.
4. Use **Copy** or **Download** to preserve the exact modal output. Back up existing files and merge fields before editing.

## Supported generator outputs

| Group platform | Client tabs | Main output |
| --- | --- | --- |
| OpenAI | Codex CLI, Codex WebSocket, Claude Code (when Messages dispatch is enabled), OpenCode | `config.toml`, `auth.json` or `experimental_bearer_token`, Anthropic environment files, `opencode.json` |
| Anthropic | Claude Code, routed Codex, OpenCode | `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, Codex Responses provider, OpenCode provider |
| Gemini | Gemini CLI, routed Codex, OpenCode | `GOOGLE_GEMINI_BASE_URL`, `GEMINI_API_KEY`, `GEMINI_MODEL` |
| Antigravity | Claude Code, Gemini CLI, routed Codex, OpenCode | `/antigravity` base path; Gemini uses `/v1beta` |
| Grok | Grok CLI, Claude Code, Codex, OpenCode | `GROK_MODELS_BASE_URL`, `XAI_API_KEY`, or the client-specific config |
| DeepSeek, MiniMax, Composite, Kimi, Zhipu, OpenCode | Claude Code, routed Codex, OpenCode | Use the generated group URL; see catalog support below |

## Codex authentication modes

OpenAI's Codex tab exposes two modes. Opening **Use key** selects **Codex CLI (WebSocket)**, **API key** and **macOS / Linux** by default:

- **Legacy** sets `requires_openai_auth = true` and offers `auth.json`. Use it only for Codex versions that require that login shape.
- **API key** sets `requires_openai_auth = false`, writes `experimental_bearer_token`, and adds the local image extension header. This stores the secret on disk; restrict permissions and never commit it.

Routed Codex tabs default to `env_key = "TOKENSAVY_API_KEY"`, `wire_api = "responses"`, and `supports_websockets = false`; the Zhipu tab embeds its API key as `experimental_bearer_token`, so its standalone `config.toml` does not depend on `TOKENSAVY_API_KEY`. The WebSocket tab enables WebSocket transport only for the OpenAI Responses path.

## Model catalog

If the selected Codex tab offers a catalog, follow the mode shown in the dialog:

- **Local file**: fetch and download codex-models.json to the path generated in model_catalog_json. Keep the path consistent if you rename the file or change CODEX_HOME.
- **Remote catalog**: when supported by the client and group, use the generated model_catalog_url to let the client request this key’s catalog. Preserve the field’s position in the generated configuration.

For tabs without a catalog, use an exact model ID available to that group. Catalog visibility, routable accounts, and protocol support are separate conditions. A visible model does not guarantee every tool or endpoint. Use the current dialog as the configuration source; a plain model-list JSON response is not a Codex manifest.

## Follow the Tokensavy UI

These screenshots render the current project's Use key component with an invalid sample key and `api.example.com`. Copy values from your own console, not the images.

1. OpenAI groups open on **Codex CLI (WebSocket)** with API key authentication. Choose **Codex CLI** if your network does not support WebSocket, or Legacy if you need the `auth.json` login shape. Each authentication mode produces different files.

![Project Codex configuration with sample data](/docs-assets/client-codex-en.png)

2. API key mode stores the key in the configuration file. Restrict file permissions and fully restart the client after downloading.

![Project Codex API key configuration with sample data](/docs-assets/client-codex-en.png)

3. For an Anthropic group, select **Claude Code** and choose the operating-system tab. Copy the complete commands and launch the client in the same terminal.

![Project Claude Code macOS / Linux configuration with sample data](/docs-assets/client-claude-en.png)

![Project Claude Code PowerShell configuration with sample data](/docs-assets/client-claude-en.png)

## Verify and troubleshoot

Send a simple message from the target client, then match its timestamp, model, key, and group in console usage. For 401, inspect whether the running process inherited the generated environment variable. For 404 or unknown-model errors, check whether `/v1` was appended twice. For 429, inspect group limits and concurrency. Desktop apps launched from a Dock or Start menu do not inherit an `export` from another terminal; launch from the configured terminal or set the client-specific environment.

Client guides:

- [Codex](/apps/codex)
- [Claude Code](/apps/claude-code)
- [Claude Desktop](/apps/claude-desktop)
- [Image skills](/apps/image-skills)
- [Codex session recovery](/apps/session-recovery-codex)
- [Claude Code session recovery](/apps/session-recovery-claude)
