#!/usr/bin/env bash
set -euo pipefail

: "${KDAN_API_KEY:?Set KDAN_API_KEY before running this installer}"
: "${KDAN_BASE_URL:?Set KDAN_BASE_URL before running this installer}"
BASE_URL="${KDAN_BASE_URL%/}"
CODEX_HOME="${CODEX_HOME:-$HOME/.codex}"
mkdir -p "$CODEX_HOME"
CONFIG="$CODEX_HOME/config.toml"
if [[ -e "$CONFIG" ]]; then cp "$CONFIG" "$CONFIG.bak.$(date +%Y%m%d%H%M%S)"; fi
cat > "$CONFIG" <<EOF
model_provider = "KDAN"
model = "${MODEL_ID:-gpt-5.6-sol}"

[model_providers.KDAN]
name = "KDAN"
base_url = "${BASE_URL%/}/v1"
env_key = "KDAN_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
EOF
chmod 600 "$CONFIG"
printf 'KDAN Codex configuration written to %s\n' "$CONFIG"
printf 'Run: KDAN_API_KEY=*** codex\n'
