#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
temporary_root="$(mktemp -d "${TMPDIR:-/tmp}/retrom-quality-sentinels.XXXXXX")"
trap 'rm -rf -- "$temporary_root"' EXIT

worktree_hash() {
  make --no-print-directory -C "$repository_root" release-input-digest
}

expect_failure() {
  local label="$1"
  local expected="$2"
  shift 2
  local output_file="$temporary_root/${label}.log"
  set +e
  "$@" >"$output_file" 2>&1
  local status=$?
  set -e
  sed "s/^/[${label}] /" "$output_file"
  if [[ $status -eq 0 ]]; then
    echo "${label}: sentinel was incorrectly accepted" >&2
    return 1
  fi
  if ! grep -Eq "$expected" "$output_file"; then
    echo "${label}: expected rejection marker not found: ${expected}" >&2
    return 1
  fi
  echo "${label}: rejected as expected (exit=${status})"
}

before_hash="$(worktree_hash)"
git -C "$repository_root" ls-files --cached --others --exclude-standard -z | \
  rsync -a --from0 --files-from=- --ignore-missing-args \
    "$repository_root/" "$temporary_root/repository/"
sentinel_root="$temporary_root/repository"
ln -s "$repository_root/web/node_modules" "$sentinel_root/web/node_modules"

cat >"$sentinel_root/internal/config/qa_sentinel.go" <<'EOF'
package config

import "os"

func qualitySentinelUnhandledError() {
	os.Chdir(".")
}
EOF
expect_failure \
  go-unhandled-error \
  'errcheck|Error return value of .os\.Chdir.' \
  bash -lc "cd '$sentinel_root' && '$repository_root/bin/golangci-lint' run ./internal/config"
rm -f -- "$sentinel_root/internal/config/qa_sentinel.go"

cat >"$sentinel_root/web/qa-sentinel.ts" <<'EOF'
async function qualitySentinelPromise(): Promise<void> {
  await Promise.resolve();
}

qualitySentinelPromise();
EOF
expect_failure \
  web-floating-promise \
  '@typescript-eslint/no-floating-promises|Promises must be awaited' \
  bash -lc "cd '$sentinel_root/web' && ./node_modules/.bin/eslint qa-sentinel.ts --max-warnings=0"
rm -f -- "$sentinel_root/web/qa-sentinel.ts"

cat >"$sentinel_root/migrations/999_qa_sentinel.sql" <<'EOF'
CREATE TABLE qa_sentinel_times (
    id INTEGER PRIMARY KEY,
    broken_at_ms TEXT NOT NULL
);
EOF
expect_failure \
  migration-text-time \
  'qa_sentinel_times\.broken_at_ms uses TEXT, want INTEGER|uses TEXT, want INTEGER' \
  bash -lc "cd '$sentinel_root' && go test ./internal/store -run '^TestMigrationsCreateIntegerBusinessTimesAndSeedCatalog$' -count=1"
rm -f -- "$sentinel_root/migrations/999_qa_sentinel.sql"

cat >"$sentinel_root/internal/importing/qa_sentinel_test.go" <<'EOF'
package importing

import "testing"

func TestQualitySentinelTraversalCannotBeAccepted(t *testing.T) {
	if _, err := ValidateLogicalPath("../escape.rom"); err != nil {
		t.Fatalf("injected traversal was rejected: %v", err)
	}
}
EOF
expect_failure \
  path-traversal \
  'injected traversal was rejected|TestQualitySentinelTraversalCannotBeAccepted' \
  bash -lc "cd '$sentinel_root' && go test ./internal/importing -run '^TestQualitySentinelTraversalCannotBeAccepted$' -count=1"
rm -f -- "$sentinel_root/internal/importing/qa_sentinel_test.go"

python3 - "$sentinel_root" <<'PY'
import importlib.util
import sys
from pathlib import Path

root = Path(sys.argv[1])
runner = root / "scripts" / "acceptance" / "run.py"
spec = importlib.util.spec_from_file_location("retrom_acceptance_runner", runner)
if spec is None or spec.loader is None:
    raise SystemExit("acceptance runner could not be loaded")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
catalog = module.all_cases()
missing_multidisc = [f"ACC-MDISC-{number:03d}" for number in range(1, 9) if f"ACC-MDISC-{number:03d}" not in catalog]
if missing_multidisc:
    raise SystemExit(f"acceptance catalog omitted multi-disc cases: {missing_multidisc}")
if len(catalog) != 137:
    raise SystemExit(f"acceptance catalog size is {len(catalog)}, want 137")
print(f"acceptance_catalog={len(catalog)}")
PY

after_hash="$(worktree_hash)"
printf 'worktree_hash_before=%s\nworktree_hash_after=%s\n' "$before_hash" "$after_hash"
if [[ "$before_hash" != "$after_hash" ]]; then
  echo "quality sentinel changed the main worktree" >&2
  exit 1
fi
