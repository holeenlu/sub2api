#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
# Even an old opt-in flag must not revive the repository-Latest installer.
if TOKENSAVY_INSTALLER_ENABLED=true bash "$ROOT_DIR/deploy/install.sh" --help; then
    echo 'Retired Tokensavy installer unexpectedly succeeded' >&2
    exit 1
fi
echo 'Tokensavy installer retirement: PASS'
