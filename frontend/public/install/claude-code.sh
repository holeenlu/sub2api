#!/usr/bin/env bash
set -euo pipefail

: "${SUB2API_API_KEY:?Set SUB2API_API_KEY before running this installer}"
: "${SUB2API_BASE_URL:?Set SUB2API_BASE_URL before running this installer}"
python3 - <<'PYTHON'
import json
import os
from pathlib import Path
import sys

home = Path(os.environ.get("CLAUDE_CONFIG_DIR", str(Path.home() / ".claude"))).expanduser()
settings = home / "settings.json"
existing = settings.exists() or settings.is_symlink()
target = home / "settings.sub2api.json" if existing else settings
# These are Claude Code's native fields; branding changes only the input variable names.
content = {"env": {
    "ANTHROPIC_BASE_URL": os.environ["SUB2API_BASE_URL"].rstrip("/"),
    "ANTHROPIC_AUTH_TOKEN": os.environ["SUB2API_API_KEY"],
}}
try:
    home.mkdir(parents=True, exist_ok=True)
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as handle:
        json.dump(content, handle, indent=2)
        handle.write("\n")
except OSError:
    sys.exit("Could not create configuration; existing files were retained. Review the target directory and any previous candidate file.")
print(f"sub2api Claude Code configuration written to {target}")
if existing:
    print("Existing settings.json, credentials and privacy settings are unchanged. Review the candidate and manually merge only the env fields you intend to use.")
else:
    print("Run: claude")
PYTHON
