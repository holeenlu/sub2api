#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

fail() {
  printf 'docker runtime resources test failed: %s\n' "$1" >&2
  exit 1
}

assert_line() {
  file=$1
  line=$2
  grep -Fqx "$line" "$file" || fail "$file is missing: $line"
}

test -s backend/resources/model-pricing/model_prices_and_context_window.json || \
  fail 'fallback pricing data is missing or empty'

assert_line Dockerfile.goreleaser 'COPY --chown=tokensavy:tokensavy backend/resources /app/resources'
assert_line deploy/Dockerfile 'COPY --from=backend-builder --chown=tokensavy:tokensavy /app/backend/resources /app/resources'
python3 - <<'PY'
from pathlib import Path
import yaml

for filename in ('.goreleaser.yaml', '.goreleaser.simple.yaml'):
    contexts = yaml.safe_load(Path(filename).read_text())['dockers']
    if not contexts:
        raise SystemExit(f'{filename}: no Docker contexts configured')
    for context in contexts:
        count = context.get('extra_files', []).count('backend/resources')
        if count != 1:
            raise SystemExit(
                f"{filename}: {context['id']} must include backend/resources exactly once; found {count}"
            )
PY

printf 'docker runtime resources test passed\n'
