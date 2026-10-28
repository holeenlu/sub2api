#!/usr/bin/env bash
set -euo pipefail

: "${TAPMODELS_API_KEY:?Set TAPMODELS_API_KEY before running this installer}"
BASE_URL="${TAPMODELS_BASE_URL:-https://tapmodels.ai}"
CODEX_HOME="${CODEX_HOME:-$HOME/.codex}"
mkdir -p "$CODEX_HOME"
CONFIG="$CODEX_HOME/config.toml"
if [[ -e "$CONFIG" ]]; then cp "$CONFIG" "$CONFIG.bak.$(date +%Y%m%d%H%M%S)"; fi
cat > "$CONFIG" <<EOF
model_provider = "tapmodels"
model = "${TAPMODELS_MODEL:-gpt-5.6-sol}"

[model_providers.tapmodels]
name = "TapModels"
base_url = "${BASE_URL%/}/v1"
env_key = "TAPMODELS_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
EOF
chmod 600 "$CONFIG"
printf 'TapModels Codex configuration written to %s\n' "$CONFIG"
printf 'Run: TAPMODELS_API_KEY=*** codex\n'
