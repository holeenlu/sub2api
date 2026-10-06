#!/usr/bin/env bash
set -euo pipefail

: "${TOKENSAVY_API_KEY:?Set TOKENSAVY_API_KEY before running this installer}"
BASE_URL="${TOKENSAVY_BASE_URL:-https://tokensavy.ai}"
CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
SETTINGS="$CLAUDE_DIR/settings.json"
mkdir -p "$CLAUDE_DIR"
if [[ -e "$SETTINGS" ]]; then cp "$SETTINGS" "$SETTINGS.bak.$(date +%Y%m%d%H%M%S)"; fi
python3 - "$SETTINGS" "$BASE_URL" "$TOKENSAVY_API_KEY" <<'PY'
import json, pathlib, sys
path, base_url, key = sys.argv[1:]
data = {}
if pathlib.Path(path).exists():
    data = json.loads(pathlib.Path(path).read_text())
env = data.setdefault("env", {})
env.update({"ANTHROPIC_BASE_URL": base_url.rstrip("/"), "ANTHROPIC_AUTH_TOKEN": key, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"})
pathlib.Path(path).write_text(json.dumps(data, indent=2) + "\n")
PY
chmod 600 "$SETTINGS"
printf 'Tokensavy Claude Code configuration written to %s\n' "$SETTINGS"
printf 'Run: claude --model ${TOKENSAVY_MODEL:-claude-sonnet-5}\n'
