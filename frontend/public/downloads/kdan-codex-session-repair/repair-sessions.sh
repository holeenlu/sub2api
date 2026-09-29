#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
PYTHON_BIN="${PYTHON_BIN:-python3}"
if ! command -v "$PYTHON_BIN" >/dev/null 2>&1; then
  printf '%s\n' "Python 3 is required. Install it, then run this script again." >&2
  exit 127
fi

exec "$PYTHON_BIN" "$SCRIPT_DIR/repair_sessions.py" "$@"
