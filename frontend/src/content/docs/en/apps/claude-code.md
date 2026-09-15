## Choose the right Claude surface

| Client | Configuration |
| --- | --- |
| Claude Code CLI | Environment variables or user settings below |
| VS Code Claude Code extension | Editor user environment settings below |
| Claude desktop with Third-Party Inference | [Desktop guide](/docs/apps/claude-desktop) |
| claude.ai browser chat | This guide does not switch browser account chats to a TapModels key |

Create a key for the target group in [API keys](/keys). Messages compatibility and client/model permissions depend on that group.

## CLI configuration

Install using the [official setup guide](https://code.claude.com/docs/en/setup), then check `claude --version`.

macOS / Linux:

```bash
export ANTHROPIC_BASE_URL="{{API_ROOT}}"
export ANTHROPIC_AUTH_TOKEN="your TapModels API key"
claude --model claude-sonnet-5
```

Windows PowerShell:

```powershell
$env:ANTHROPIC_BASE_URL="{{API_ROOT}}"
$env:ANTHROPIC_AUTH_TOKEN="your TapModels API key"
claude --model claude-sonnet-5
```

Replace the example model with an enabled ID. Use the root URL; the client adds `/v1/messages`. Enter the raw key without a `Bearer ` prefix. Back up conflicting configuration before changing it; a saved official login need not be deleted.

## Persistent settings and IDE

Back up and merge this `env` block into `~/.claude/settings.json`:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "{{API_ROOT}}",
    "ANTHROPIC_AUTH_TOKEN": "your TapModels API key"
  }
}
```

This is private configuration. Do not put credentials in shared project `.claude/settings.json`. GUI editors may not inherit terminal exports. In VS Code user Settings JSON:

```json
{
  "claudeCode.environmentVariables": [
    { "name": "ANTHROPIC_BASE_URL", "value": "{{API_ROOT}}" },
    { "name": "ANTHROPIC_AUTH_TOKEN", "value": "your TapModels API key" }
  ]
}
```

## Verify and choose a model

Use `/status` to inspect the base URL and credential source. Choose an exact enabled ID with `/model claude-sonnet-5`, send a simple message and verify TapModels usage. The model picker may not automatically enumerate every `/v1/models` entry.

Claude Code may send auxiliary title, summary and token-count requests. If only those fail, review the group's allowlist or mappings.

## Resume after a switch

Return to the original project directory and use `claude --resume`, `claude --continue`, or `/resume`. Check the OS user, project path and configuration directory. The downloadable Codex repair utility is not a Claude repair tool.

| Error | Action |
| --- | --- |
| 401 | Inspect key, settings overrides and credential source in /status |
| 404 / model error | Check root URL and enabled model |
| 429 | Respect limits and check balance, concurrency and quota |
| Login prompt persists | Confirm client and environment; desktop uses a separate configuration |

Sources: [gateway setup](https://code.claude.com/docs/en/llm-gateway-connect), [resume conversations](https://code.claude.com/docs/en/common-workflows#resume-previous-conversations). Project configuration checked 2026-09-15.
