#!/usr/bin/env bash

set -euo pipefail

mode="${1:---quick}"
case "$mode" in
  --quick|--full) ;;
  *)
    echo "usage: $0 [--quick|--full]" >&2
    exit 2
    ;;
esac

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
cd "$repo_root"

required_files=(
  "AGENTS.md"
  "docs/architecture/overview.md"
  "docs/architecture/invariants.md"
  "docs/architecture/request-lifecycle.md"
  "docs/architecture-health.md"
  "docs/adr/README.md"
  ".agents/skills/architecture-maintenance/SKILL.md"
)

for path in "${required_files[@]}"; do
  if [[ ! -s "$path" ]]; then
    echo "missing required architecture file: $path" >&2
    exit 1
  fi
done

git diff --check

if rg -n '^(<<<<<<< .+|=======|>>>>>>> .+)$' \
  --glob '!.git/**' \
  --glob '!frontend/node_modules/**' \
  --glob '!frontend/dist/**' \
  .; then
  echo "unresolved merge marker found" >&2
  exit 1
fi

bash -n scripts/verify-architecture.sh

while IFS= read -r shell_file; do
  bash -n "$shell_file"
done < <(find deploy backend/scripts -type f -name '*.sh' -print 2>/dev/null | sort)

if [[ "$mode" == "--quick" ]]; then
  echo "architecture verification passed (quick)"
  exit 0
fi

(cd backend && go test ./...)
(cd frontend && pnpm run typecheck)
(cd frontend && pnpm run test:run)
(cd frontend && pnpm run build)

echo "architecture verification passed (full)"
