## Choose the right Claude surface

| Client | Configuration |
| --- | --- |
| Claude Code CLI | Environment variables or user settings below |
| VS Code Claude Code extension | Editor user environment settings below |
| Claude desktop with Third-Party Inference | [Desktop guide](/apps/claude-desktop) |
| claude.ai browser chat | This guide does not switch browser account chats to a KDAN key |

Create a key for the target group in [API keys](/keys). Messages compatibility and client/model permissions depend on that group.

## CLI configuration

### Online install (macOS / Linux)

```bash
export KDAN_BASE_URL="{{API_ROOT}}"
export KDAN_API_KEY="YOUR_KDAN_API_KEY"
curl -fsSL {{API_ROOT}}/install/claude-code.sh | bash
```

The script backs up `~/.claude/settings.json`, writes the Messages environment variables, and restricts file permissions. Review it before piping into a shell; `KDAN_BASE_URL` is required.

This is the project's **Use key → Claude Code** UI with invalid sample credentials and an example URL. Choose the operating-system tab and copy values from your own console. The [console guide](/apps/console) also includes the PowerShell screenshot.

![Project Claude Code configuration with sample data](/docs-assets/client-claude-en.png)

Install using the [official setup guide](https://code.claude.com/docs/en/setup), then check `claude --version`.

macOS / Linux:

```bash
export ANTHROPIC_BASE_URL="{{API_ROOT}}"
export ANTHROPIC_AUTH_TOKEN="your API key"
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
claude --model claude-sonnet-5
```

Windows PowerShell:

```powershell
$env:ANTHROPIC_BASE_URL="{{API_ROOT}}"
$env:ANTHROPIC_AUTH_TOKEN="your API key"
$env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="1"
claude --model claude-sonnet-5
```

Replace the example model with an enabled ID. Use the root URL; the client adds `/v1/messages`. Enter the raw key without a `Bearer ` prefix. Back up conflicting configuration before changing it; a saved official login need not be deleted.

## Persistent settings and IDE

Back up and merge this `env` block into `~/.claude/settings.json`:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "{{API_ROOT}}",
    "ANTHROPIC_AUTH_TOKEN": "your API key",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"
  }
}
```

This is private configuration. Do not put credentials in shared project `.claude/settings.json`. `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` is emitted by the current console builder to reduce sign-in, telemetry, and other nonessential traffic. It does not route Claude web, Remote Control, or voice services through KDAN. GUI editors may not inherit terminal exports. In VS Code user Settings JSON:

```json
{
  "claudeCode.environmentVariables": [
    { "name": "ANTHROPIC_BASE_URL", "value": "{{API_ROOT}}" },
    { "name": "ANTHROPIC_AUTH_TOKEN", "value": "your API key" },
    { "name": "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "value": "1" }
  ]
}
```

## Verify and choose a model

Use `/status` to inspect the base URL and credential source. Choose an exact enabled ID with `/model claude-sonnet-5`, send a simple message and verify KDAN usage. The model picker may not automatically enumerate every `/v1/models` entry.

Claude Code may send auxiliary title, summary and token-count requests. If only those fail, review the group's allowlist or mappings.

## Resume after a switch

Return to the original project directory and use `claude --resume`, `claude --continue`, or `/resume`. Check the OS user, project path and configuration directory. If the session is still missing, follow [Claude Code session recovery](/apps/session-recovery-claude); do not use the Codex repair utility on Claude data.

| Error | Action |
| --- | --- |
| 401 | Inspect key, settings overrides and credential source in /status |
| 404 / model error | Check root URL and enabled model |
| 429 | Respect limits and check balance, concurrency and quota |
| Login prompt persists | Confirm client and environment; desktop uses a separate configuration |

Sources: [gateway setup](https://code.claude.com/docs/en/llm-gateway-connect), [resume conversations](https://code.claude.com/docs/en/common-workflows#resume-previous-conversations). Project configuration checked 2026-09-15.
