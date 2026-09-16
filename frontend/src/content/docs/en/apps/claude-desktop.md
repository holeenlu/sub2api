## Scope

This guide applies to Claude desktop builds with **Third-Party Inference** configuration. Desktop routing is separate from Claude Code CLI; shell exports and `~/.claude/settings.json` do not configure it.

These steps follow official documentation and the this project Messages routes. A real desktop-to-production-key test has not been completed. If your build lacks this setting, use [Claude Code](/apps/claude-code).

## Connect

1. Open Help → Troubleshooting → Enable Developer Mode and restart when prompted.
2. Open Developer → Configure Third-Party Inference.
3. Choose **Gateway** and enter the values below.
4. Save or apply as instructed by the app, open a local session and send a test message.

| Field | Value |
| --- | --- |
| Gateway base URL | `{{API_ROOT}}` |
| Credential kind | Static API key |
| Gateway API key | Your this project key |
| Gateway auth scheme | Bearer; the project also accepts x-api-key |
| Model | An enabled Messages-compatible model in the key's group |

Managed settings can make the form read-only; contact your administrator. Do not enter a gateway key into OAuth or OIDC sign-in fields.

## Verify

Match the test request in this project usage records. Model access follows the key's group. Messages compatibility does not establish compatibility with every desktop plugin or cloud feature.

For Gateway was unreachable, check the root URL and network. For 401, check the key. For missing models, inspect group access and explicit client model settings. See official guidance for remote/cloud restrictions. Official account cloud history and local gateway sessions are not guaranteed to migrate; our repair package handles only Codex local indexes.

Sources: [Claude desktop gateway](https://claude.com/docs/third-party/claude-desktop/gateway), [client differences](https://code.claude.com/docs/en/llm-gateway-connect#desktop-app), checked 2026-09-15.
