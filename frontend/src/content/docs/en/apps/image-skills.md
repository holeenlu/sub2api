## Downloadable skills

this project provides two separate skills:

| Skill | Fixed model | Generate | Edit |
| --- | --- | --- | --- |
| `gpt-image-flare` | `gpt-image-2.5-flare` | `/v1/images/generations` | `/v1/images/edits` |
| `gpt-image-sunburst` | `gpt-image-2.5-sunburst` | `/v1/images/generations` | `/v1/images/edits` |

Download the [Flare skill](/downloads/gpt-image-flare.zip) or [Sunburst skill](/downloads/gpt-image-sunburst.zip). Each bundle includes `SKILL.md`, Codex interface metadata, and the Python request script.

## Install in Codex desktop

1. Fully quit Codex.
2. Extract the bundle and confirm that its top-level directory is `gpt-image-flare/` or `gpt-image-sunburst/`, with `SKILL.md` directly inside.
3. Move that directory under `~/.agents/skills/`; the final path is `~/.agents/skills/gpt-image-flare/SKILL.md` or `~/.agents/skills/gpt-image-sunburst/SKILL.md`.
4. Restart Codex and invoke `$gpt-image-flare` or `$gpt-image-sunburst` in a task.

## Install for Codex CLI

macOS or Linux:

```bash
mkdir -p "$HOME/.agents/skills"
unzip -q -o ./gpt-image-flare.zip -d "$HOME/.agents/skills"
test -f "$HOME/.agents/skills/gpt-image-flare/SKILL.md"
python3 -m venv "$HOME/.agents/skills/gpt-image-flare/.venv"
"$HOME/.agents/skills/gpt-image-flare/.venv/bin/python" -m pip install -r "$HOME/.agents/skills/gpt-image-flare/requirements.txt"
"$HOME/.agents/skills/gpt-image-flare/.venv/bin/python" "$HOME/.agents/skills/gpt-image-flare/scripts/generate.py" --check-config
```

Windows PowerShell:

```powershell
New-Item -ItemType Directory -Force "$env:USERPROFILE\.agents\skills" | Out-Null
Expand-Archive -Force .\gpt-image-flare.zip "$env:USERPROFILE\.agents\skills"
if (-not (Test-Path "$env:USERPROFILE\.agents\skills\gpt-image-flare\SKILL.md")) { throw "Invalid skill archive layout" }
py -3 -m venv "$env:USERPROFILE\.agents\skills\gpt-image-flare\.venv"
& "$env:USERPROFILE\.agents\skills\gpt-image-flare\.venv\Scripts\python.exe" -m pip install -r "$env:USERPROFILE\.agents\skills\gpt-image-flare\requirements.txt"
& "$env:USERPROFILE\.agents\skills\gpt-image-flare\.venv\Scripts\python.exe" "$env:USERPROFILE\.agents\skills\gpt-image-flare\scripts\generate.py" --check-config
```

The script reads `base_url` and `env_key` from the top-level Codex `model_provider` / `model_providers`; it does not support profile-based provider selection or place the key in generated files. When only `API_KEY` is set, the provider still supplies both the URL and the configured environment-variable name. Set `API_BASE_URL` only when intentionally overriding the provider; that override also requires `API_KEY`. Provider URLs must use HTTPS (HTTP is allowed only for loopback tests), and redirects are rejected.

## Runtime and credentials

Image scripts use these credential sources, independently of Codex login files:

| Codex configuration | Image script behavior |
| --- | --- |
| Provider has `env_key` | Use that variable; fail if missing |
| Provider has `experimental_bearer_token` | Use the token in that provider |
| Legacy key exists only in `auth.json` | The script does not read this file; use the explicit environment override below |

For Legacy authentication or a separate image group, set both values in the process that runs the skill. The key must belong to a group with the image model enabled. Setting only the key does not replace the provider authentication mode.

```bash
export API_BASE_URL="{{API_ROOT}}"
export API_KEY="YOUR_IMAGE_GROUP_API_KEY"
```

PowerShell uses `$env:API_BASE_URL="{{API_ROOT}}"` and `$env:API_KEY="YOUR_IMAGE_GROUP_API_KEY"`. Launch desktop Codex from that terminal as described in the [Codex guide](/apps/codex).

Use Python 3.11+ and install Pillow from the bundled `requirements.txt`, preferably in a virtual environment inside the skill directory. On Windows use `py -3` and the environment's `Scripts/python.exe`; on macOS/Linux use its `bin/python`.

An explicit `API_BASE_URL` enables a complete environment override and requires `API_KEY`; without it, the script reads the active Codex provider's URL and credential fields listed above. A missing configured key variable fails instead of borrowing another account key. `--check-config` validates configuration only; `--dry-run` validates the payload and input images without a network call. Real calls need credentials, network and model access. Use an absolute output path; existing files require explicit `--force`.

Current official guidance recommends `~/.agents/skills`. Older clients may discover `~/.codex/skills`; use the path your client actually supports, without duplicate names. Type `$gpt-image-flare` to check discovery and restart if needed.

## Use the skill

Generate an image:

```text
$gpt-image-flare Generate a precise product image on white, 1024x1024, high quality.
```

For editing, attach a local image and state both the requested change and what must remain. The skill uses the edit endpoint when an input image is present. Every execution makes one billable request and does not retry automatically; the result checks the actual file format and dimensions.

Installation source: [Codex skills](https://developers.openai.com/codex/skills/). Scripts are validated locally with simulated requests; real availability follows the key group.
