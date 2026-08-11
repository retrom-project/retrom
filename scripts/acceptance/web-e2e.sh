#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
temporary_root="$(mktemp -d "${TMPDIR:-/tmp}/retrom-web-e2e.XXXXXX")"
backend_port=18084
web_port=13004
backend_origin="http://127.0.0.1:${backend_port}"
web_origin="http://localhost:${web_port}"
process_id=""

cleanup() {
  if [[ -n "$process_id" ]]; then
    RETROM_DATA_DIR="$temporary_root/data" "$repository_root/scripts/dev.sh" --stop 2>/dev/null || true
    wait "$process_id" 2>/dev/null || true
  fi
  rm -rf -- "$temporary_root"
  rm -rf -- "$repository_root/web/.next-e2e"
}
trap cleanup EXIT

for port in "$backend_port" "$web_port"; do
  if ss -ltn "sport = :${port}" | tail -n +2 | grep -q .; then
    echo "web E2E port is already in use: ${port}" >&2
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
  NEXT_DIST_DIR=".next-e2e" \
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
    echo "timed out waiting for web E2E server" >&2
    exit 1
  fi
  sleep 0.2
done

RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
RETROM_ACCEPTANCE_BACKEND="$backend_origin" \
  scripts/acceptance/http-flow.sh
scripts/acceptance/seed-review-queue.sh "$temporary_root/data/retrom.db"
scripts/acceptance/seed-run-blocker.sh "$temporary_root/data/retrom.db"

(cd web && \
  RETROM_WEB_ORIGIN="$web_origin" \
  RETROM_E2E_DATABASE="$temporary_root/data/retrom.db" \
  E2E_SERVER_IMPORT_SEED="1" \
  npm run test:e2e)

RETROM_DATA_DIR="$temporary_root/data" "$repository_root/scripts/dev.sh" --stop
set +e
wait "$process_id"
set -e
process_id=""
deadline=$((SECONDS + 5))
while ss -ltn "sport = :${backend_port} or sport = :${web_port}" | tail -n +2 | grep -q .; do
  if (( SECONDS >= deadline )); then
    echo "web E2E server left listeners behind" >&2
    exit 1
  fi
  sleep 0.1
done
printf 'web_e2e=passed\nserver_data_root=temporary\nchildren_remaining=0\n'
