#!/usr/bin/env bash
set -euo pipefail

case_id="${1:-}"
if [[ ! "$case_id" =~ ^ACC-NP-00(1|2|3|4|5|6|7|8|9)$ ]]; then
  echo "usage: netplay-case.sh ACC-NP-001|ACC-NP-002|ACC-NP-003|ACC-NP-004|ACC-NP-005|ACC-NP-006|ACC-NP-007|ACC-NP-008|ACC-NP-009" >&2
  exit 2
fi
keep_acceptance="${RETROM_KEEP_ACCEPTANCE:-0}"
unset RETROM_KEEP_ACCEPTANCE

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PATH="$repository_root/.cache/tools/node-v24.18.0-linux-x64/bin:$PATH"
export PATH
temporary_root="$(mktemp -d "${TMPDIR:-/tmp}/retrom-netplay-acceptance.XXXXXX")"
backend_port=18086
web_port=13006
backend_origin="http://127.0.0.1:${backend_port}"
web_origin="http://localhost:${web_port}"
process_id=""
dev_state="$temporary_root/dev-state"

cleanup() {
  if [[ -n "$process_id" ]]; then
    RETROM_DEV_STATE_DIR="$dev_state" RETROM_DATA_DIR="$temporary_root/data" \
      "$repository_root/scripts/dev.sh" --stop 2>/dev/null || true
    wait "$process_id" 2>/dev/null || true
  fi
  if [[ "$keep_acceptance" == "1" ]]; then
    printf 'retained_netplay_acceptance=%s\n' "$temporary_root" >&2
  else
    rm -rf -- "$temporary_root"
  fi
  rm -rf -- "$repository_root/web/.next-netplay-acceptance"
}
trap cleanup EXIT

cd "$repository_root"
python3 data/example/verify-fixtures.py >/dev/null

for port in "$backend_port" "$web_port"; do
  if ss -ltn "sport = :${port}" | tail -n +2 | grep -q .; then
    echo "netplay acceptance port is already in use: ${port}" >&2
    exit 1
  fi
done

mkdir -p "$temporary_root/data"
setsid make dev \
  RETROM_MODE="test" \
  RETROM_NETPLAY_ENABLED="true" \
  RETROM_DEV_STATE_DIR="$dev_state" \
  RETROM_DATA_DIR="$temporary_root/data" \
  RETROM_HTTP_ADDR="127.0.0.1:${backend_port}" \
  RETROM_PUBLIC_ORIGIN="$web_origin" \
  NEXT_DEV_HOST="127.0.0.1" \
  NEXT_DEV_PORT="$web_port" \
  NEXT_DIST_DIR=".next-netplay-acceptance" \
  NEXT_BACKEND_ORIGIN="$backend_origin" \
  >"$temporary_root/server.log" 2>&1 &
process_id=$!

deadline=$((SECONDS + 90))
until curl --fail --silent "$backend_origin/health/ready" >/dev/null 2>&1 &&
  curl --fail --silent "$web_origin/netplay" >/dev/null 2>&1; do
  if ! kill -0 "$process_id" 2>/dev/null; then
    sed 's/^/[server] /' "$temporary_root/server.log" >&2
    exit 1
  fi
  if (( SECONDS >= deadline )); then
    sed 's/^/[server] /' "$temporary_root/server.log" >&2
    echo "timed out waiting for netplay acceptance server" >&2
    exit 1
  fi
  sleep 0.2
done

python3 scripts/acceptance/seed-netplay.py "$temporary_root/data/retrom.db" "$temporary_root/data"

if ! (cd web && \
  RETROM_WEB_ORIGIN="$web_origin" \
  RETROM_E2E_DATABASE="$temporary_root/data/retrom.db" \
  RETROM_NETPLAY_ACCEPTANCE="1" \
  RETROM_ACCEPTANCE_CASE_DIR="${RETROM_ACCEPTANCE_CASE_DIR:-}" \
  npm exec -- playwright test e2e/netplay.spec.ts --grep "$case_id" --project=chrome-1280 \
    --workers=1 --timeout=300000); then
  sed 's/^/[server] /' "$temporary_root/server.log" >&2
  exit 1
fi

RETROM_DEV_STATE_DIR="$dev_state" RETROM_DATA_DIR="$temporary_root/data" \
  "$repository_root/scripts/dev.sh" --stop
set +e
wait "$process_id"
set -e
process_id=""

deadline=$((SECONDS + 5))
while ss -ltn "sport = :${backend_port} or sport = :${web_port}" | tail -n +2 | grep -q .; do
  if (( SECONDS >= deadline )); then
    echo "netplay acceptance server left listeners behind" >&2
    exit 1
  fi
  sleep 0.1
done
printf 'case=%s\nindependent_chrome_processes=2\nchildren_remaining=0\n' "$case_id"
