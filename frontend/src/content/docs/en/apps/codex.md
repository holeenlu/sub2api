## Prepare a key and model

Create a key in [API keys](/keys), choose its group, and open **Use key → Codex**. Prefer the generated configuration; model catalogs are supported only for OpenAI/Composite groups. Replace the example `gpt-5.6-sol` with an enabled model. Back up existing configuration and merge these fields; do not replace the whole file. HTTP/SSE and WebSocket configurations occupy separate console tabs; start with HTTP/SSE.

This guide covers local clients that read Codex configuration. Remote hosts, WSL and containers need configuration at their execution location; cloud tasks may not use your local file.

## CLI setup

### Online install (macOS / Linux)

```bash
export TAPMODELS_API_KEY="YOUR_TAPMODELS_API_KEY"
curl -fsSL https://tapmodels.ai/install/codex.sh | bash
```

The script backs up `config.toml`, writes the Responses provider, and sets mode `600`. Review it before piping into a shell; never put a key in shell history or source control. Set `TAPMODELS_BASE_URL` for a custom gateway and `TAPMODELS_MODEL` to override the example model.

This screenshot shows the project's OpenAI-group API key configuration with invalid sample credentials and an example URL. Copy your own group configuration. The [console guide](/apps/console) also shows Legacy mode and the steps; click images to view full size.

![Project Codex API key configuration with sample data](/docs-assets/client-codex-en.png)

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

Merge this routed-group example into `~/.codex/config.toml`, or your `CODEX_HOME` directory. It applies to Anthropic, Gemini, Grok, and other groups routed into Codex through Responses. Top-level fields must precede tables and each provider table must appear only once. Copy the current model and `base_url` from the modal rather than inferring them from this example:

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

Run `codex` in the same terminal. Disabling WebSockets provides an HTTP/SSE starting point; it does not mean the gateway lacks WebSocket routes.

The OpenAI group generates a different shape: provider ID `OpenAI`, response-storage and network settings, the model catalog, and `[features]`. The default **Legacy** mode downloads `config.toml` plus `auth.json` and sets `requires_openai_auth = true`. **API key** mode instead sets `requires_openai_auth = false` and `experimental_bearer_token`; fully restart Codex after switching. Do not combine the two modes or merge a routed `tapmodels` provider table into the OpenAI group's `OpenAI` provider.

## Desktop: make the key available

A desktop app opened from the Dock or Start menu does not automatically inherit a variable exported in another terminal. Quit existing processes, then launch the installed executable directly from the terminal that contains the key. If installed as `/Applications/Codex.app` on macOS:

```bash
"/Applications/Codex.app/Contents/MacOS/Codex"
```

Check the actual installation path first. On Windows, invoke the installed Codex `.exe` from the PowerShell that contains `$env:TAPMODELS_API_KEY`.

For an icon launch, select **API key** in the OpenAI group's Use key modal and download its complete `config.toml`. This stores a secret in the file; restrict access and never commit it. For other groups, keep the generated `env_key` configuration and do not add a second authentication field yourself.

## Group model catalog

This section applies only to OpenAI/Composite groups. Other routed groups do not support dedicated catalog downloads and should not set `model_catalog_json`. Query ordinary `GET /v1/models` and set `model` to an exact ID; do not save that list response as a Codex manifest.

Download `codex-models.json` from **API keys → Use key → Codex**, save it in a stable location, and add this at the top level:

```toml
model_catalog_json = "/absolute/path/.codex/codex-models.json"
```

Windows TOML can use a literal path such as `model_catalog_json = 'C:\Users\your-name\.codex\codex-models.json'`. Refresh the catalog when group access changes. If download fails, diagnose with the minimal configuration first.

## Verify and troubleshoot

Send a simple message in a new task and match its timestamp, key and model in TapModels usage records. Then resume existing work. See [Codex session recovery](/apps/session-recovery-codex) if history disappears.

| Symptom | Check |
| --- | --- |
| 401 / API_KEY_REQUIRED | Key variable in the actual process, or token in the active provider |
| Unknown model | Key group, exact ID, stale catalog |
| Settings ignored | CODEX_HOME, selected profile, project overrides, remote host |
| No Speed/Fast control | Catalog capabilities; a billing multiplier does not create a client control |
| Missing old tasks | Original project, archives, provider and local data directory |

Based on the project's Use key generator and [Codex configuration reference](https://developers.openai.com/codex/config-reference/), checked 2026-09-15.
