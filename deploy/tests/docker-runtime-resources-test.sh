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

assert_line Dockerfile 'COPY --from=builder --chown=sub2api:sub2api /src/backend/resources /app/resources'
assert_line Dockerfile 'COPY --from=pg-client /usr/local/bin/pg_dump /usr/local/bin/pg_dump'
assert_line Dockerfile 'COPY --from=pg-client /usr/local/bin/psql /usr/local/bin/psql'
assert_line Dockerfile 'COPY LICENSE /app/LICENSE'

printf 'docker runtime resources test passed\n'
