## Client tools

Before downloading, read the [console configuration builder](/apps/console) and [app guides](/apps) to confirm the key group and client support.

| File | Purpose | Default behavior |
| --- | --- | --- |
| [KDAN Codex session repair bundle](/downloads/kdan-codex-session-repair.zip) | macOS, Linux, and Windows session diagnostics and repair | Read-only; explicit Apply required to write |
| [GPT Image 2.5 Flare skill](/downloads/gpt-image-flare.zip) | Codex image generation and editing | Fixed Flare model |
| [GPT Image 2.5 Sunburst skill](/downloads/gpt-image-sunburst.zip) | Codex image generation and editing | Fixed Sunburst model |

## Inspect after downloading

These archives are static assets published by this site. Extract and inspect `README.md` or the skill's top-level `SKILL.md` before installing; no script should contain your API key. The recovery bundle only operates on local Codex data. An image skill uses the active Codex provider for one explicit billable request.

Verify the downloaded archive against [SHA256SUMS.txt](/downloads/SHA256SUMS.txt) before extracting it.

Do not install same-named scripts from chat attachments, file-sharing mirrors, or unrelated sites. Keep the previous skill directory and a `~/.codex` backup before upgrading.
