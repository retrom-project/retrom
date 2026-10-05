#!/usr/bin/env bash
set -euo pipefail

case_id="${1:-}"
if [[ ! "$case_id" =~ ^(ACC-PLAT-007|ACC-UI-(00[1-9]|01[012])|ACC-RUN-(00[2346789]|01[0129])|ACC-SAVE-002|ACC-FAV-00[34]|ACC-TAG-005|ACC-BIOS-00[67]|ACC-PEG-00[567]|ACC-BASIC-001|ACC-ES-00[56]|ACC-IMM-(00[1-9]|01[01])|ACC-MOB-00[1-7]|ACC-MEDIA-001|ACC-NP-(01[456789]|02[012]))$ ]]; then
  echo "usage: ui-case.sh ACC-PLAT-007|ACC-UI-001..012|ACC-RUN-002..004|ACC-RUN-006..012|ACC-RUN-019|ACC-SAVE-002|ACC-FAV-003|ACC-FAV-004|ACC-TAG-005|ACC-BIOS-006|ACC-BIOS-007|ACC-PEG-005|ACC-PEG-006|ACC-PEG-007|ACC-BASIC-001|ACC-ES-005|ACC-ES-006|ACC-IMM-001..011|ACC-MOB-001..007|ACC-MEDIA-001|ACC-NP-014..022" >&2
  exit 2
fi

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$repository_root/scripts/acceptance/dev-dist-cleanup.sh"
"$repository_root/scripts/prepare-postgres-tools.sh"
export PATH="$repository_root/.cache/tools/postgres-python/bin:$PATH"
PATH="$repository_root/.cache/tools/node-v24.18.0-linux-x64/bin:$PATH"
export PATH
temporary_root="$(mktemp -d "${TMPDIR:-/tmp}/retrom-ui-acceptance.XXXXXX")"
backend_port="${RETROM_ACCEPTANCE_BACKEND_PORT:-18082}"
web_port="${RETROM_ACCEPTANCE_WEB_PORT:-13002}"
server_ready_timeout="${RETROM_ACCEPTANCE_SERVER_READY_TIMEOUT_SECONDS:-150}"
if [[ ! "$backend_port" =~ ^[0-9]+$ || ! "$web_port" =~ ^[0-9]+$ ]] ||
  (( backend_port < 1024 || backend_port > 65535 || web_port < 1024 || web_port > 65535 || backend_port == web_port )); then
  echo "acceptance ports must be distinct integers between 1024 and 65535" >&2
  exit 2
fi
if [[ ! "$server_ready_timeout" =~ ^[0-9]+$ ]] || (( server_ready_timeout < 30 || server_ready_timeout > 600 )); then
  echo "acceptance server ready timeout must be an integer between 30 and 600 seconds" >&2
  exit 2
fi
backend_origin="http://127.0.0.1:${backend_port}"
web_origin="http://localhost:${web_port}"
process_id=""
acceptance_dist_dir=".next-acceptance-${case_id,,}"
dev_state="$temporary_root/dev-state"
cp -p "$repository_root/web/next-env.d.ts" "$temporary_root/next-env.d.ts"
cp -p "$repository_root/web/tsconfig.json" "$temporary_root/tsconfig.json"

cleanup() {
  local status=$?
  trap - EXIT
  if [[ -n "$process_id" ]]; then
    RETROM_DEV_STATE_DIR="$dev_state" "$repository_root/scripts/dev.sh" --stop 2>/dev/null || true
    wait "$process_id" 2>/dev/null || true
  fi
  cp -p "$temporary_root/next-env.d.ts" "$repository_root/web/next-env.d.ts"
  cp -p "$temporary_root/tsconfig.json" "$repository_root/web/tsconfig.json"
  if ! remove_dev_dist "$repository_root/web/$acceptance_dist_dir"; then
    status=1
  fi
  if (( status != 0 )) && [[ -f "$temporary_root/server.log" ]]; then
    local failure_directory
    mkdir -p "$repository_root/.cache/retrom/acceptance"
    failure_directory="$(mktemp -d "$repository_root/.cache/retrom/acceptance/ui-case-failure-XXXXXX")"
    cp -p "$temporary_root/server.log" "$failure_directory/server.log"
    printf 'ui_case_failure_evidence=%s\n' "$failure_directory" >&2
  fi
  if [[ -f "$temporary_root/data/postgres-test.json" ]]; then
    python3 "$repository_root/scripts/acceptance/postgres_fixture.py" drop "$temporary_root/data"
  fi || status=1
  rm -rf -- "$temporary_root"
  exit "$status"
}
trap cleanup EXIT

for port in "$backend_port" "$web_port"; do
  if ss -ltn "sport = :${port}" | tail -n +2 | grep -q .; then
    echo "acceptance port is already in use: ${port}" >&2
    exit 1
  fi
done
mkdir -p "$temporary_root/data"
RETROM_DATABASE_URL="$(python3 "$repository_root/scripts/acceptance/postgres_fixture.py" create "$temporary_root/data")"
export RETROM_DATABASE_URL
mkdir -p "$temporary_root/source/BIOS"
mkdir -p "$temporary_root/source/Games/media/Acceptance Game"
printf 'collection: NES\ngame: Acceptance Game\ndescription: Pegasus UI acceptance fixture\nfile: acceptance.nes\n' >"$temporary_root/source/Games/metadata.pegasus.txt"
printf 'retrom deterministic pegasus acceptance fixture\n' >"$temporary_root/source/Games/acceptance.nes"
printf '\000\000\000\030ftypisom\000\000\000\000isommp42' >"$temporary_root/source/Games/media/Acceptance Game/video.mp4"
"$repository_root/scripts/acceptance/prepare-pegasus-gba-source.sh" "$temporary_root/source/Playable"
"$repository_root/scripts/acceptance/prepare-emulationstation-gba-source.sh" "$temporary_root/source/EmulationStationPlayable"
if [[ "$case_id" == "ACC-BASIC-001" ]]; then
  mkdir -p "$temporary_root/source/Basic/nested"
  cp -p "$repository_root/testdata/public-roms/gba-smoke/pegasus-smoke.gba" "$temporary_root/source/Basic/nested/basic-smoke.GBA"
  printf 'ignored text\n' >"$temporary_root/source/Basic/notes.txt"
  printf 'no extension\n' >"$temporary_root/source/Basic/README"
fi
cd "$repository_root"
NEXT_WEB_E2E=true setsid make dev \
  RETROM_MODE="test" \
  RETROM_DEV_STATE_DIR="$dev_state" \
  RETROM_DATA_DIR="$temporary_root/data" \
  RETROM_HTTP_ADDR="127.0.0.1:${backend_port}" \
  RETROM_PUBLIC_ORIGIN="$web_origin" \
  RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE="http://{launchId}.rpg.localhost:${backend_port}" \
  NEXT_DEV_HOST="127.0.0.1" \
  NEXT_DEV_PORT="$web_port" \
  NEXT_DEV_DIST_DIR="$acceptance_dist_dir" \
  NEXT_BACKEND_ORIGIN="$backend_origin" \
  >"$temporary_root/server.log" 2>&1 &
process_id=$!

deadline=$((SECONDS + server_ready_timeout))
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

deadline=$((SECONDS + 90))
until grep -q '"msg":"background DAT indexing complete"' "$temporary_root/server.log"; do
  if ! kill -0 "$process_id" 2>/dev/null; then
    sed 's/^/[server] /' "$temporary_root/server.log" >&2
    exit 1
  fi
  if (( SECONDS >= deadline )); then
    sed 's/^/[server] /' "$temporary_root/server.log" >&2
    echo "timed out waiting for background DAT indexing" >&2
    exit 1
  fi
  sleep 0.2
done

RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
  scripts/acceptance/http-flow.sh

if [[ "$case_id" == "ACC-UI-011" ]]; then
  # Complete the cold webpack route build before the browser navigation budget.
  ui_warmup_start=$SECONDS
  warmup_cookies="$temporary_root/ui-warmup-cookies.txt"
  curl --fail --silent --show-error --max-time 30 --cookie-jar "$warmup_cookies" \
    --header 'Content-Type: application/json' --header "Origin: $web_origin" \
    --data '{"username":"test","password":"test"}' \
    --output /dev/null "$web_origin/api/v1/auth/login"
  warmup_status="$(curl --fail --silent --show-error --max-time 30 --cookie "$warmup_cookies" \
    --output /dev/null --write-out '%{http_code}' "$web_origin/immersive")"
  [[ "$warmup_status" == "200" ]] || { echo "UI warmup failed: HTTP $warmup_status" >&2; exit 1; }
  printf 'ui_warmup=complete route=/immersive http_status=200 duration_seconds=%s\n' "$((SECONDS - ui_warmup_start))"
fi

if [[ "$case_id" == "ACC-IMM-009" ]]; then
  python3 scripts/acceptance/seed-immersive-library.py "$temporary_root/data" \
    >"$temporary_root/immersive-library-seed.json"
fi

core_expansion_result="$temporary_root/core-expansion.json"
if [[ "$case_id" == "ACC-RUN-019" ]]; then
  for fixture_id in fceumm nestopia; do
    RETROM_ACCEPTANCE_ORIGIN="$web_origin" RETROM_ACCEPTANCE_RESULT_FILE="$temporary_root/$fixture_id.json" \
      scripts/acceptance/console-flow.sh "$fixture_id"
  done
  cat "$temporary_root/fceumm.json" "$temporary_root/nestopia.json" >"$core_expansion_result"
fi
if [[ "$case_id" =~ ^ACC-RUN-0(08|09|10|11|12)$ ]]; then
  case "$case_id" in
    ACC-RUN-008) fixture_id="snes9x" ;;
    ACC-RUN-009) fixture_id="nestopia" ;;
    ACC-RUN-010) fixture_id="mame2003_plus" ;;
    ACC-RUN-011) fixture_id="fbalpha2012_cps1" ;;
    ACC-RUN-012) fixture_id="fbalpha2012_cps2" ;;
  esac
  if [[ "$fixture_id" == "snes9x" || "$fixture_id" == "nestopia" ]]; then
    RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
    RETROM_ACCEPTANCE_RESULT_FILE="$core_expansion_result" \
      scripts/acceptance/console-flow.sh "$fixture_id"
  else
    go run scripts/acceptance/seed-public-arcade-dat.go \
      --database "$RETROM_DATABASE_URL" --fixture "$fixture_id"
    RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
    RETROM_ACCEPTANCE_RESULT_FILE="$core_expansion_result" \
      scripts/acceptance/arcade-flow.sh "$fixture_id"
  fi
fi

if [[ "$case_id" == "ACC-RUN-006" ]]; then
  go run scripts/acceptance/seed-public-arcade-dat.go \
    --database "$RETROM_DATABASE_URL" --fixture mame2003
  RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
  RETROM_ACCEPTANCE_RESULT_FILE="$temporary_root/mame2003.json" \
    scripts/acceptance/arcade-flow.sh mame2003
  python3 scripts/acceptance/seed-arcade-current-launch.py "$temporary_root/data" mame2003
fi

if [[ "$case_id" == "ACC-IMM-002" ]]; then
  immersive_fixture_index=0
  for fixture_id in mame2003 fbneo mame2003_plus; do
    if (( immersive_fixture_index % 2 == 0 )); then
      immersive_cover="$repository_root/testdata/public-roms/gba-smoke/gba-smoke-cover.png"
    else
      immersive_cover="$repository_root/testdata/public-roms/gba-smoke/emulationstation-smoke-cover.png"
    fi
    go run scripts/acceptance/seed-public-arcade-dat.go \
      --database "$RETROM_DATABASE_URL" --fixture "$fixture_id"
    RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
    RETROM_ACCEPTANCE_COVER_PATH="$immersive_cover" \
    RETROM_ACCEPTANCE_RESULT_FILE="$temporary_root/$fixture_id.json" \
      scripts/acceptance/arcade-flow.sh "$fixture_id"
    immersive_fixture_index=$((immersive_fixture_index + 1))
  done
fi

if [[ "$case_id" == "ACC-IMM-006" ]]; then
  go run scripts/acceptance/seed-public-arcade-dat.go \
    --database "$RETROM_DATABASE_URL" --fixture mame2003
  RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
  RETROM_ACCEPTANCE_RESULT_FILE="$temporary_root/mame2003.json" \
    scripts/acceptance/arcade-flow.sh mame2003
fi

if [[ "$case_id" == "ACC-IMM-006" ]]; then
  go run scripts/acceptance/seed-public-arcade-dat.go \
    --database "$RETROM_DATABASE_URL" --fixture fbneo
  RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
  RETROM_ACCEPTANCE_RESULT_FILE="$temporary_root/fbneo.json" \
    scripts/acceptance/arcade-flow.sh fbneo
fi

if [[ "$case_id" == "ACC-RUN-007" ]]; then
  go run scripts/acceptance/seed-public-arcade-dat.go \
    --database "$RETROM_DATABASE_URL" --fixture fbneo
  RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
  RETROM_ACCEPTANCE_RESULT_FILE="$temporary_root/fbneo.json" \
    scripts/acceptance/arcade-flow.sh fbneo
  python3 scripts/acceptance/seed-arcade-current-launch.py "$temporary_root/data" fbneo
fi

if [[ "$case_id" == "ACC-BIOS-007" ]]; then
  python3 scripts/acceptance/seed-bios-catalog.py "$temporary_root/data" 286
fi

if [[ "$case_id" == "ACC-UI-008" || "$case_id" == "ACC-UI-010" ]]; then
  scripts/acceptance/seed-review-queue.sh "$temporary_root/data"
fi
if [[ "$case_id" == "ACC-RUN-004" ]]; then
  scripts/acceptance/seed-run-blocker.sh "$temporary_root/data"
fi
if [[ "$case_id" == "ACC-FAV-003" ]]; then
  scripts/acceptance/seed-favorites-user-flow.sh "$temporary_root/data"
fi

if [[ "$case_id" == "ACC-UI-005" ]]; then
  python3 scripts/acceptance/seed-ui-home.py "$temporary_root/data" populated
fi

export RETROM_ACCEPTANCE_DATA_DIR="$temporary_root/data"

specification="e2e/acceptance.spec.ts"
if [[ "$case_id" == "ACC-PLAT-007" ]]; then
  specification="e2e/directory-groups.spec.ts"
fi
if [[ "$case_id" == "ACC-UI-011" ]]; then
  specification="e2e/ui-consistency.spec.ts"
fi
if [[ "$case_id" == "ACC-UI-009" ]]; then
  specification="e2e/auth.spec.ts"
fi
if [[ "$case_id" == "ACC-FAV-003" || "$case_id" == "ACC-FAV-004" ]]; then
  specification="e2e/favorites.spec.ts"
fi
if [[ "$case_id" == "ACC-TAG-005" ]]; then
  specification="e2e/tags.spec.ts"
fi
if [[ "$case_id" == "ACC-BIOS-006" || "$case_id" == "ACC-BIOS-007" || "$case_id" == "ACC-PEG-005" || "$case_id" == "ACC-PEG-006" || "$case_id" == "ACC-MEDIA-001" ]]; then
  specification="e2e/server-import.spec.ts"
fi
if [[ "$case_id" == "ACC-BASIC-001" ]]; then
  specification="e2e/basic-import.spec.ts"
fi
if [[ "$case_id" == "ACC-ES-005" || "$case_id" == "ACC-ES-006" ]]; then
  specification="e2e/emulationstation-import.spec.ts"
fi
if [[ "$case_id" =~ ^ACC-IMM-00[1-8]$ ]]; then
  specification="e2e/immersive.spec.ts"
fi
if [[ "$case_id" =~ ^ACC-IMM-(009|010|011)$ ]]; then
  specification="e2e/immersive-library.spec.ts"
fi
if [[ "$case_id" =~ ^ACC-MOB-00[1-7]$ ]]; then
  specification="e2e/mobile.spec.ts"
fi
if [[ "$case_id" == "ACC-PEG-007" || "$case_id" == "ACC-RUN-019" || "$case_id" == "ACC-UI-012" ]]; then
  specification="e2e/issue-regressions.spec.ts"
fi
playwright_grep="$case_id"
if [[ "$case_id" == "ACC-UI-010" ]]; then
  # ACC-UI-008 performs the stateful draft/decision setup consumed by 010.
  playwright_grep="ACC-UI-008|ACC-UI-010"
fi
specifications=("$specification")
if [[ "$case_id" == "ACC-MEDIA-001" ]]; then
  specifications+=("e2e/review-video.spec.ts")
fi
if [[ "$case_id" == "ACC-UI-008" ]]; then
  specifications+=("e2e/review-detail-layout.spec.ts")
fi
if [[ "$case_id" == "ACC-UI-003" ]]; then
  specifications+=("e2e/game-detail-layout.spec.ts" "e2e/game-detail-media-regressions.spec.ts")
fi
if [[ "$case_id" == "ACC-UI-005" ]]; then
  specifications+=("e2e/home-description.spec.ts" "e2e/admin-game-detail.spec.ts")
fi
if [[ "$case_id" == "ACC-UI-001" ]]; then
  specifications+=("e2e/navigation-errors.spec.ts" "e2e/library-platform-scrollbar.spec.ts")
fi
if [[ "$case_id" == "ACC-MOB-007" ]]; then
  specifications=("e2e/mobile.spec.ts" "e2e/acceptance.spec.ts" "e2e/immersive.spec.ts")
  playwright_grep="ACC-MOB-007|ACC-UI-005|ACC-UI-006|ACC-UI-007|ACC-IMM-007"
fi
playwright_args=(playwright test "${specifications[@]}" --grep "$playwright_grep")
if [[ "$case_id" == "ACC-UI-011" || "$case_id" == "ACC-PLAT-007" || "$case_id" == "ACC-PEG-007" ]]; then
  playwright_args+=(--project=chrome-1280 --project=chrome-4k-150)
fi
if [[ "$case_id" =~ ^ACC-MOB-00[1-6]$ ]]; then
  playwright_args+=(--project=chrome-mobile)
elif [[ "$case_id" != "ACC-PEG-007" && "$case_id" != "ACC-UI-011" && "$case_id" != "ACC-PLAT-007" && "$case_id" != "ACC-UI-005" && "$case_id" != "ACC-UI-006" && "$case_id" != "ACC-UI-009" && "$case_id" != "ACC-FAV-004" && "$case_id" != "ACC-BIOS-006" && "$case_id" != "ACC-PEG-005" && "$case_id" != "ACC-ES-005" && "$case_id" != "ACC-IMM-007" && "$case_id" != "ACC-MOB-007" && "$case_id" != "ACC-MEDIA-001" ]]; then
  playwright_args+=(--project=chrome-1280)
else
  playwright_args+=(--workers=1)
fi
core_expansion_results='[]'
if [[ -f "$core_expansion_result" ]]; then
  core_expansion_results="$(jq -sc '.' "$core_expansion_result")"
fi
# Chrome's Unix SingletonSocket must fit sockaddr_un even when build/data
# TMPDIR points into a deep PFB worktree. Only browser temporaries use /tmp;
# the Case data root and server processes retain the caller's chosen TMPDIR.
(cd web && \
  TMPDIR=/tmp \
  RETROM_WEB_ORIGIN="$web_origin" \
  RETROM_E2E_SERVER_SOURCE="$temporary_root/source" \
  RETROM_E2E_DATA_ROOT="$temporary_root/data" \
  RETROM_MAME2003_PLATFORM_INSTANCE_ID="$(jq -r '.platformInstanceId // empty' "$temporary_root/mame2003.json" 2>/dev/null || true)" \
  RETROM_FBNEO_PLATFORM_INSTANCE_ID="$(jq -r '.platformInstanceId // empty' "$temporary_root/fbneo.json" 2>/dev/null || true)" \
  RETROM_CORE_EXPANSION_RESULTS="$core_expansion_results" \
  RETROM_ACCEPTANCE_CASE_DIR="${RETROM_ACCEPTANCE_CASE_DIR:-}" \
  npm exec -- "${playwright_args[@]}")

if [[ "$case_id" == "ACC-UI-003" ]]; then
  RETROM_ACCEPTANCE_BASE_URL="$web_origin" \
  RETROM_ACCEPTANCE_USERNAME=test RETROM_ACCEPTANCE_PASSWORD=test \
  TMPDIR=/tmp node scripts/acceptance/dos_launch_options_ui.mjs
fi

RETROM_DEV_STATE_DIR="$dev_state" "$repository_root/scripts/dev.sh" --stop
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
