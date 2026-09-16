#!/usr/bin/env bash
set -euo pipefail

: "${API_KEY:?Set API_KEY before running this installer}"
BASE_URL="${API_BASE_URL:-https://api.example.com}"
CODEX_HOME="${CODEX_HOME:-$HOME/.codex}"
mkdir -p "$CODEX_HOME"
CONFIG="$CODEX_HOME/config.toml"
if [[ -e "$CONFIG" ]]; then cp "$CONFIG" "$CONFIG.bak.$(date +%Y%m%d%H%M%S)"; fi
cat > "$CONFIG" <<EOF
model_provider = "sub2api"
model = "${MODEL_ID:-gpt-5.6-sol}"

[model_providers.sub2api]
name = "Sub2API"
base_url = "${BASE_URL%/}/v1"
env_key = "API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
EOF
chmod 600 "$CONFIG"
printf 'Sub2API Codex configuration written to %s\n' "$CONFIG"
printf 'Run: API_KEY=*** codex\n'
