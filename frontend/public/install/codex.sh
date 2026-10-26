#!/usr/bin/env bash
set -euo pipefail

: "${TOKENSAVY_API_KEY:?Set TOKENSAVY_API_KEY before running this installer}"
BASE_URL="${TOKENSAVY_BASE_URL:-https://tokensavy.ai}"
CODEX_HOME="${CODEX_HOME:-$HOME/.codex}"
mkdir -p "$CODEX_HOME"
CONFIG="$CODEX_HOME/config.toml"
if [[ -e "$CONFIG" ]]; then cp "$CONFIG" "$CONFIG.bak.$(date +%Y%m%d%H%M%S)"; fi
cat > "$CONFIG" <<EOF
model_provider = "tokensavy"
model = "${TOKENSAVY_MODEL:-gpt-5.6-sol}"

[model_providers.tokensavy]
name = "Tokensavy"
base_url = "${BASE_URL%/}/v1"
env_key = "TOKENSAVY_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
EOF
chmod 600 "$CONFIG"
printf 'Tokensavy Codex configuration written to %s\n' "$CONFIG"
printf 'Run: TOKENSAVY_API_KEY=*** codex\n'
