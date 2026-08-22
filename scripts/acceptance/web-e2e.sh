#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
temporary_root="$(mktemp -d "${TMPDIR:-/tmp}/retrom-web-e2e.XXXXXX")"
backend_port=18084
web_port=13004
backend_origin="http://127.0.0.1:${backend_port}"
web_origin="http://localhost:${web_port}"
process_id=""
dev_state="$temporary_root/dev-state"
cp -p "$repository_root/web/next-env.d.ts" "$temporary_root/next-env.d.ts"
cp -p "$repository_root/web/tsconfig.json" "$temporary_root/tsconfig.json"

chrome_executable="${RETROM_CHROME_EXECUTABLE:-$repository_root/.cache/tools/retrom-chrome-for-testing}"
if [[ ! -x "$chrome_executable" ]]; then
  echo "Chrome for Testing is missing; run make install-deps" >&2
  exit 1
fi
chrome_version="$($chrome_executable --version | sed -E 's/[[:space:]]+$//')"
if [[ "$chrome_version" != Google\ Chrome\ * ]]; then
  echo "RETROM_CHROME_EXECUTABLE must be Google Chrome, got: $chrome_version" >&2
  exit 1
fi
export RETROM_CHROME_EXECUTABLE="$chrome_executable"
printf 'browser=%s\n' "$chrome_version"

remove_e2e_dist() {
  local dist_directory="$repository_root/web/.next-e2e"
  local deadline=$((SECONDS + 5))
  while [[ -e "$dist_directory" ]]; do
    rm -rf -- "$dist_directory" 2>/dev/null || true
    [[ ! -e "$dist_directory" ]] && return 0
    if (( SECONDS >= deadline )); then
      echo "failed to remove web E2E build directory: $dist_directory" >&2
      return 1
    fi
    sleep 0.1
  done
}

cleanup() {
  local status=$?
  trap - EXIT
  if [[ -n "$process_id" ]]; then
    RETROM_DEV_STATE_DIR="$dev_state" RETROM_DATA_DIR="$temporary_root/data" "$repository_root/scripts/dev.sh" --stop 2>/dev/null || true
    wait "$process_id" 2>/dev/null || true
  fi
  cp -p "$temporary_root/next-env.d.ts" "$repository_root/web/next-env.d.ts"
  cp -p "$temporary_root/tsconfig.json" "$repository_root/web/tsconfig.json"
  rm -rf -- "$temporary_root"
  if ! remove_e2e_dist; then
    status=1
  fi
  exit "$status"
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
mkdir -p "$temporary_root/source/Games/media/Acceptance Game"
printf 'collection: NES\ngame: Acceptance Game\ndescription: Pegasus E2E fixture\nfile: acceptance.nes\n' >"$temporary_root/source/Games/metadata.pegasus.txt"
printf 'retrom deterministic pegasus acceptance fixture\n' >"$temporary_root/source/Games/acceptance.nes"
printf '\000\000\000\030ftypisom\000\000\000\000isommp42' >"$temporary_root/source/Games/media/Acceptance Game/video.mp4"
"$repository_root/scripts/acceptance/prepare-pegasus-gba-source.sh" "$temporary_root/source/Playable"
cd "$repository_root"
RETROM_SERVER_IMPORT_ROOTS="[{\"id\":\"pegasus-bios\",\"label\":\"Pegasus BIOS\",\"path\":\"$temporary_root/source\"}]" \
setsid make dev \
  RETROM_MODE="test" \
  RETROM_DEV_STATE_DIR="$dev_state" \
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
RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
RETROM_ACCEPTANCE_BACKEND="$backend_origin" \
RETROM_ACCEPTANCE_RESULT_FILE="$temporary_root/netplay-nes.json" \
  scripts/acceptance/netplay-nes-flow.sh
go run scripts/acceptance/seed-public-arcade-dat.go \
  "$temporary_root/data/retrom.db" \
  mame2003 \
  "$repository_root/testdata/public-roms/arcade-smoke/mame2003-smoke.xml" \
  >"$temporary_root/mame2003-smoke-dat.json"
RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
RETROM_ACCEPTANCE_BACKEND="$backend_origin" \
RETROM_ACCEPTANCE_RESULT_FILE="$temporary_root/mame2003.json" \
  scripts/acceptance/arcade-flow.sh mame2003
go run scripts/acceptance/seed-public-arcade-dat.go \
  "$temporary_root/data/retrom.db" \
  fbneo \
  "$repository_root/testdata/public-roms/arcade-smoke/fbneo/fbneo-smoke.dat" \
  >"$temporary_root/fbneo-smoke-dat.json"
RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
RETROM_ACCEPTANCE_BACKEND="$backend_origin" \
RETROM_ACCEPTANCE_RESULT_FILE="$temporary_root/netplay-fbneo.json" \
  scripts/acceptance/arcade-flow.sh fbneo
python3 scripts/acceptance/seed-arcade-schema-v2-launch.py "$temporary_root/data/retrom.db" mame2003
python3 scripts/acceptance/seed-arcade-schema-v2-launch.py "$temporary_root/data/retrom.db" fbneo
scripts/acceptance/seed-review-queue.sh "$temporary_root/data/retrom.db"
scripts/acceptance/seed-run-blocker.sh "$temporary_root/data/retrom.db"

(cd web && \
  RETROM_WEB_ORIGIN="$web_origin" \
  RETROM_E2E_DATABASE="$temporary_root/data/retrom.db" \
  RETROM_NETPLAY_NES_GAME_ID="$(jq -r .gameId "$temporary_root/netplay-nes.json")" \
  RETROM_NETPLAY_NES_FIXTURE_SHA256="$(jq -r .fixtureSha256 "$temporary_root/netplay-nes.json")" \
  RETROM_NETPLAY_FBNEO_GAME_ID="$(jq -r .gameId "$temporary_root/netplay-fbneo.json")" \
  RETROM_NETPLAY_FBNEO_FIXTURE_SHA256="$(jq -r .fixtureSha256 "$temporary_root/netplay-fbneo.json")" \
  RETROM_MAME2003_PLATFORM_INSTANCE_ID="$(jq -r .platformInstanceId "$temporary_root/mame2003.json")" \
  E2E_SERVER_IMPORT_SEED="1" \
  npm run test:e2e)

RETROM_DEV_STATE_DIR="$dev_state" RETROM_DATA_DIR="$temporary_root/data" "$repository_root/scripts/dev.sh" --stop
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
