#!/usr/bin/env bash
set -euo pipefail

case_id="${1:-}"
if [[ ! "$case_id" =~ ^(ACC-UI-00[1-9]|ACC-RUN-00[234]|ACC-SAVE-002|ACC-FAV-00[34]|ACC-BIOS-00[67])$ ]]; then
  echo "usage: ui-case.sh ACC-UI-00N|ACC-RUN-002|ACC-RUN-003|ACC-RUN-004|ACC-SAVE-002|ACC-FAV-003|ACC-FAV-004|ACC-BIOS-006|ACC-BIOS-007" >&2
  exit 2
fi
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PATH="$repository_root/.cache/tools/node-v24.18.0-linux-x64/bin:$PATH"
export PATH
temporary_root="$(mktemp -d "${TMPDIR:-/tmp}/retrom-ui-acceptance.XXXXXX")"
backend_port=18082
web_port=13002
backend_origin="http://127.0.0.1:${backend_port}"
web_origin="http://localhost:${web_port}"
process_id=""

cleanup() {
  if [[ -n "$process_id" ]]; then
    "$repository_root/scripts/dev.sh" --stop 2>/dev/null || true
    wait "$process_id" 2>/dev/null || true
  fi
  rm -rf -- "$temporary_root"
}
trap cleanup EXIT

for port in "$backend_port" "$web_port"; do
  if ss -ltn "sport = :${port}" | tail -n +2 | grep -q .; then
    echo "acceptance port is already in use: ${port}" >&2
    exit 1
  fi
done
mkdir -p "$temporary_root/data"
mkdir -p "$temporary_root/source/BIOS"
cd "$repository_root"
RETROM_SERVER_IMPORT_ROOTS="[{\"id\":\"pegasus-bios\",\"label\":\"Pegasus BIOS\",\"path\":\"$temporary_root/source\"}]" \
setsid make dev \
  RETROM_MODE="test" \
  RETROM_DATA_DIR="$temporary_root/data" \
  RETROM_HTTP_ADDR="127.0.0.1:${backend_port}" \
  RETROM_PUBLIC_ORIGIN="$web_origin" \
  NEXT_DEV_HOST="127.0.0.1" \
  NEXT_DEV_PORT="$web_port" \
  NEXT_BACKEND_ORIGIN="$backend_origin" \
  >"$temporary_root/server.log" 2>&1 &
process_id=$!

deadline=$((SECONDS + 90))
until curl --fail --silent "$backend_origin/health/ready" >/dev/null 2>&1 &&
  curl --fail --silent "$web_origin" >/dev/null 2>&1; do
  if ! kill -0 "$process_id" 2>/dev/null; then
    sed 's/^/[server] /' "$temporary_root/server.log" >&2
    exit 1
  fi
  if (( SECONDS >= deadline )); then
    sed 's/^/[server] /' "$temporary_root/server.log" >&2
    echo "timed out waiting for UI acceptance server" >&2
    exit 1
  fi
  sleep 0.2
done

RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
RETROM_ACCEPTANCE_BACKEND="$backend_origin" \
  scripts/acceptance/http-flow.sh

if [[ "$case_id" == "ACC-BIOS-007" ]]; then
  python3 scripts/acceptance/seed-bios-catalog.py "$temporary_root/data/retrom.db" 286
fi

if [[ "$case_id" == "ACC-UI-008" ]]; then
  scripts/acceptance/seed-review-queue.sh "$temporary_root/data/retrom.db"
fi
if [[ "$case_id" == "ACC-RUN-004" ]]; then
  scripts/acceptance/seed-run-blocker.sh "$temporary_root/data/retrom.db"
fi
if [[ "$case_id" == "ACC-FAV-003" ]]; then
  scripts/acceptance/seed-favorites-user-flow.sh "$temporary_root/data/retrom.db"
fi

specification="e2e/acceptance.spec.ts"
if [[ "$case_id" == "ACC-UI-009" ]]; then
  specification="e2e/auth.spec.ts"
fi
if [[ "$case_id" == "ACC-FAV-003" || "$case_id" == "ACC-FAV-004" ]]; then
  specification="e2e/favorites.spec.ts"
fi
if [[ "$case_id" == "ACC-BIOS-006" || "$case_id" == "ACC-BIOS-007" ]]; then
  specification="e2e/server-import.spec.ts"
fi
playwright_args=(playwright test "$specification" --grep "$case_id")
if [[ "$case_id" != "ACC-UI-005" && "$case_id" != "ACC-UI-006" && "$case_id" != "ACC-UI-009" && "$case_id" != "ACC-FAV-004" && "$case_id" != "ACC-BIOS-006" ]]; then
  playwright_args+=(--project=chrome-1280)
else
  playwright_args+=(--workers=1)
fi
(cd web && \
  RETROM_WEB_ORIGIN="$web_origin" \
  RETROM_E2E_DATABASE="$temporary_root/data/retrom.db" \
  RETROM_ACCEPTANCE_CASE_DIR="${RETROM_ACCEPTANCE_CASE_DIR:-}" \
  npm exec -- "${playwright_args[@]}")

"$repository_root/scripts/dev.sh" --stop
set +e
wait "$process_id"
set -e
process_id=""
deadline=$((SECONDS + 5))
while ss -ltn "sport = :${backend_port} or sport = :${web_port}" | tail -n +2 | grep -q .; do
  if (( SECONDS >= deadline )); then
    echo "UI acceptance server left listeners behind" >&2
    exit 1
  fi
  sleep 0.1
done
printf 'case=%s\nserver_data_root=temporary\nchildren_remaining=0\n' "$case_id"
