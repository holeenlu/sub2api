## Prepare a key and model

Create a key in [API keys](/keys), choose its group, and open **Use key → Codex**. Prefer the configuration and model catalog generated there. Replace the example `gpt-5.6-sol` with an enabled model. Back up existing configuration and merge these fields; do not replace the whole file.

This guide covers local clients that read Codex configuration. Remote hosts, WSL and containers need configuration at their execution location; cloud tasks may not use your local file.

## CLI setup

Install the CLI using the [official instructions](https://developers.openai.com/codex/cli/) and verify with `codex --version`.

macOS / Linux:

```bash
export TAPMODELS_API_KEY="your TapModels API key"
mkdir -p "${CODEX_HOME:-$HOME/.codex}"
```

Windows PowerShell:

```powershell
$env:TAPMODELS_API_KEY="your TapModels API key"
$codexConfigDir = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $env:USERPROFILE '.codex' }
New-Item -ItemType Directory -Force $codexConfigDir | Out-Null
notepad (Join-Path $codexConfigDir 'config.toml')
```

Merge this into `~/.codex/config.toml`, or your `CODEX_HOME` directory. Top-level fields must precede tables, and each provider table must appear only once:

```toml
model_provider = "tapmodels"
model = "gpt-5.6-sol"

[model_providers.tapmodels]
name = "TapModels"
base_url = "{{API_ROOT}}/v1"
env_key = "TAPMODELS_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
```

Run `codex` in the same terminal. Disabling WebSockets provides an HTTP/SSE starting point; it does not mean the gateway lacks WebSocket routes. Do not edit `auth.json` to rotate this key.

## Desktop: make the key available

A desktop app opened from the Dock or Start menu does not automatically inherit a variable exported in another terminal. Quit existing processes, then launch the installed executable directly from the terminal that contains the key. If installed as `/Applications/Codex.app` on macOS:

```bash
"/Applications/Codex.app/Contents/MacOS/Codex"
```

Check the actual installation path first. On Windows, invoke the installed Codex `.exe` from the PowerShell that contains `$env:TAPMODELS_API_KEY`.

For icon launches without environment propagation, the console's direct-token option can replace `env_key` with `experimental_bearer_token = "your key"` in that provider table. Choose one authentication method. This stores a secret in the file; restrict access and never commit it. Official documentation supports this fallback but discourages it.

## Group model catalog

Download `codex-models.json` from **API keys → Use key → Codex**, save it in a stable location, and add this at the top level:

```toml
model_catalog_json = "/absolute/path/.codex/codex-models.json"
```

Windows TOML can use a literal path such as `model_catalog_json = 'C:\Users\your-name\.codex\codex-models.json'`. Refresh the catalog when group access changes. If download fails, diagnose with the minimal configuration first.

## Verify and troubleshoot

Send a simple message in a new task and match its timestamp, key and model in TapModels usage records. Then resume existing work. See [session recovery](/docs/apps/session-recovery) if history disappears.

| Symptom | Check |
| --- | --- |
| 401 / API_KEY_REQUIRED | Key variable in the actual process, or token in the active provider |
| Unknown model | Key group, exact ID, stale catalog |
| Settings ignored | CODEX_HOME, selected profile, project overrides, remote host |
| No Speed/Fast control | Catalog capabilities; a billing multiplier does not create a client control |
| Missing old tasks | Original project, archives, provider and local data directory |

Based on the project's Use key generator and [Codex configuration reference](https://developers.openai.com/codex/config-reference/), checked 2026-09-15.
