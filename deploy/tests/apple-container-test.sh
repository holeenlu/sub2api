#!/bin/bash

set -euo pipefail

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "${TEST_DIR}/.." && pwd)"
SCRIPT="${DEPLOY_DIR}/apple-container.sh"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/kdan-apple-test.XXXXXX")"
STATE_DIR="${TEST_ROOT}/state"
ENV_FILE="${TEST_ROOT}/kdan.env"

cleanup() {
    rm -rf "${TEST_ROOT}"
}
trap cleanup EXIT

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_exists() {
    [[ -e "$1" ]] || fail "Expected path to exist: $1"
}

assert_missing() {
    [[ ! -e "$1" ]] || fail "Expected path to be absent: $1"
}

export FAKE_CONTAINER_STATE="${STATE_DIR}"
export PATH="${TEST_DIR}/fixtures/bin:${PATH}"
export KDAN_ENV_FILE="${ENV_FILE}"

mkdir -p "${STATE_DIR}"

"${SCRIPT}" init
[[ "$(stat -f '%Lp' "${ENV_FILE}")" == "600" ]] || fail "init did not create a mode-600 env file"
grep -q '^POSTGRES_PASSWORD=change_this_secure_password$' "${ENV_FILE}" && fail "init retained the placeholder password"

chmod 644 "${ENV_FILE}"
if "${SCRIPT}" up >/dev/null 2>&1; then
    fail "up accepted an insecure env file"
fi
chmod 600 "${ENV_FILE}"

"${SCRIPT}" up
assert_exists "${STATE_DIR}/containers/kdan-apple"
assert_exists "${STATE_DIR}/containers/kdan-apple-postgres"
assert_exists "${STATE_DIR}/containers/kdan-apple-redis"
assert_exists "${STATE_DIR}/running/kdan-apple"
"${SCRIPT}" status >/dev/null

"${SCRIPT}" up --recreate
assert_exists "${STATE_DIR}/running/kdan-apple"
"${SCRIPT}" down
assert_missing "${STATE_DIR}/running/kdan-apple"
assert_missing "${STATE_DIR}/running/kdan-apple-postgres"
assert_missing "${STATE_DIR}/running/kdan-apple-redis"

"${SCRIPT}" destroy --yes
assert_missing "${STATE_DIR}/containers/kdan-apple"
assert_missing "${STATE_DIR}/networks/kdan-apple"
assert_exists "${STATE_DIR}/volumes/kdan-apple-data"

"${SCRIPT}" up
"${SCRIPT}" destroy --volumes --yes
assert_missing "${STATE_DIR}/volumes/kdan-apple-data"
assert_missing "${STATE_DIR}/volumes/kdan-apple-postgres-data"
assert_missing "${STATE_DIR}/volumes/kdan-apple-redis-data"

touch "${STATE_DIR}/system-running"
touch "${STATE_DIR}/containers/kdan-apple"
touch "${STATE_DIR}/unowned/container/kdan-apple"
if "${SCRIPT}" status >/dev/null 2>&1; then
    fail "status accepted an unowned same-name container"
fi

printf 'Apple container lifecycle tests passed.\n'
