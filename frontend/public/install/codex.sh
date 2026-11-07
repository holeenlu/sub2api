#!/usr/bin/env bash
set -euo pipefail

: "${SUB2API_API_KEY:?Set SUB2API_API_KEY before running this installer}"
: "${SUB2API_BASE_URL:?Set SUB2API_BASE_URL before running this installer}"
python3 - <<'PYTHON'
import json
import os
from pathlib import Path
import sys

home = Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex"))).expanduser()
config = home / "config.toml"
existing = config.exists() or config.is_symlink()
target = home / "config.sub2api.toml" if existing else config
base_url = os.environ["SUB2API_BASE_URL"].rstrip("/")
if not base_url.endswith("/v1"):
    base_url += "/v1"
quote = json.dumps
content = '\n'.join([
    'model_provider = "sub2api"',
    'model = ' + quote(os.environ.get("MODEL_ID", "gpt-5.6-sol")),
    '',
    '[model_providers.sub2api]',
    'name = "sub2api"',
    'base_url = ' + quote(base_url),
    'env_key = "SUB2API_API_KEY"',
    'wire_api = "responses"',
    'requires_openai_auth = false',
    'supports_websockets = false',
    '',
])
try:
    home.mkdir(parents=True, exist_ok=True)
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as handle:
        handle.write(content)
except OSError:
    sys.exit("Could not create configuration; existing files were retained. Review the target directory and any previous candidate file.")
print(f"sub2api Codex configuration written to {target}")
if existing:
    print("Existing config.toml and provider selection are unchanged. Review the candidate and manually merge only the settings you intend to use.")
else:
    print("Run codex from the terminal where SUB2API_API_KEY is exported.")
PYTHON
