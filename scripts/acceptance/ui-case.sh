#!/usr/bin/env bash
set -euo pipefail

case_id="${1:-}"
if [[ ! "$case_id" =~ ^(ACC-UI-(00[1-9]|010)|ACC-RUN-(00[2346789]|01[012])|ACC-SAVE-002|ACC-FAV-00[34]|ACC-TAG-005|ACC-BIOS-00[67]|ACC-PEG-00[56]|ACC-ES-00[56]|ACC-IMM-00[1-8]|ACC-MEDIA-001|ACC-STOR-001|ACC-NP-(01[456789]|02[012]))$ ]]; then
  echo "usage: ui-case.sh ACC-UI-001..010|ACC-RUN-002..004|ACC-RUN-006..012|ACC-SAVE-002|ACC-FAV-003|ACC-FAV-004|ACC-TAG-005|ACC-BIOS-006|ACC-BIOS-007|ACC-PEG-005|ACC-PEG-006|ACC-ES-005|ACC-ES-006|ACC-IMM-001..008|ACC-MEDIA-001|ACC-STOR-001|ACC-NP-014..022" >&2
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
acceptance_dist_dir=".next-acceptance-${case_id,,}"
dev_state="$temporary_root/dev-state"
cp -p "$repository_root/web/next-env.d.ts" "$temporary_root/next-env.d.ts"
cp -p "$repository_root/web/tsconfig.json" "$temporary_root/tsconfig.json"

cleanup() {
  local status=$?
  trap - EXIT
  if (( status != 0 )) && [[ -f "$temporary_root/server.log" ]]; then
    local failure_directory
    mkdir -p "$repository_root/.cache/retrom/acceptance"
    failure_directory="$(mktemp -d "$repository_root/.cache/retrom/acceptance/ui-case-failure-XXXXXX")"
    cp -p "$temporary_root/server.log" "$failure_directory/server.log"
    printf 'ui_case_failure_evidence=%s\n' "$failure_directory" >&2
  fi
  if [[ -n "$process_id" ]]; then
    RETROM_DEV_STATE_DIR="$dev_state" "$repository_root/scripts/dev.sh" --stop 2>/dev/null || true
    wait "$process_id" 2>/dev/null || true
  fi
  cp -p "$temporary_root/next-env.d.ts" "$repository_root/web/next-env.d.ts"
  cp -p "$temporary_root/tsconfig.json" "$repository_root/web/tsconfig.json"
  rm -rf -- "$temporary_root"
  rm -rf -- "$repository_root/web/$acceptance_dist_dir"
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
mkdir -p "$temporary_root/source/BIOS"
mkdir -p "$temporary_root/source/Games/media/Acceptance Game"
printf 'collection: NES\ngame: Acceptance Game\ndescription: Pegasus UI acceptance fixture\nfile: acceptance.nes\n' >"$temporary_root/source/Games/metadata.pegasus.txt"
printf 'retrom deterministic pegasus acceptance fixture\n' >"$temporary_root/source/Games/acceptance.nes"
printf '\000\000\000\030ftypisom\000\000\000\000isommp42' >"$temporary_root/source/Games/media/Acceptance Game/video.mp4"
"$repository_root/scripts/acceptance/prepare-pegasus-gba-source.sh" "$temporary_root/source/Playable"
"$repository_root/scripts/acceptance/prepare-emulationstation-gba-source.sh" "$temporary_root/source/EmulationStationPlayable"
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
  NEXT_DIST_DIR="$acceptance_dist_dir" \
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

core_expansion_result="$temporary_root/core-expansion.json"
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
    RETROM_ACCEPTANCE_BACKEND="$backend_origin" \
    RETROM_ACCEPTANCE_RESULT_FILE="$core_expansion_result" \
      scripts/acceptance/netplay-nes-flow.sh "$fixture_id"
  else
    go run scripts/acceptance/seed-public-arcade-dat.go \
      --database "$temporary_root/data/retrom.db" --fixture "$fixture_id"
    RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
    RETROM_ACCEPTANCE_BACKEND="$backend_origin" \
    RETROM_ACCEPTANCE_RESULT_FILE="$core_expansion_result" \
      scripts/acceptance/arcade-flow.sh "$fixture_id"
  fi
fi

if [[ "$case_id" == "ACC-RUN-006" ]]; then
  go run scripts/acceptance/seed-public-arcade-dat.go \
    --database "$temporary_root/data/retrom.db" --fixture mame2003
  RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
  RETROM_ACCEPTANCE_BACKEND="$backend_origin" \
  RETROM_ACCEPTANCE_RESULT_FILE="$temporary_root/mame2003.json" \
    scripts/acceptance/arcade-flow.sh mame2003
  python3 scripts/acceptance/seed-arcade-schema-v2-launch.py "$temporary_root/data/retrom.db" mame2003
fi

if [[ "$case_id" == "ACC-IMM-002" || "$case_id" == "ACC-IMM-006" ]]; then
  go run scripts/acceptance/seed-public-arcade-dat.go \
    --database "$temporary_root/data/retrom.db" --fixture mame2003
  RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
  RETROM_ACCEPTANCE_BACKEND="$backend_origin" \
  RETROM_ACCEPTANCE_RESULT_FILE="$temporary_root/mame2003.json" \
    scripts/acceptance/arcade-flow.sh mame2003
fi

if [[ "$case_id" == "ACC-IMM-006" ]]; then
  go run scripts/acceptance/seed-public-arcade-dat.go \
    --database "$temporary_root/data/retrom.db" --fixture fbneo
  RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
  RETROM_ACCEPTANCE_BACKEND="$backend_origin" \
  RETROM_ACCEPTANCE_RESULT_FILE="$temporary_root/fbneo.json" \
    scripts/acceptance/arcade-flow.sh fbneo
fi

if [[ "$case_id" == "ACC-RUN-007" ]]; then
  go run scripts/acceptance/seed-public-arcade-dat.go \
    --database "$temporary_root/data/retrom.db" --fixture fbneo
  RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
  RETROM_ACCEPTANCE_BACKEND="$backend_origin" \
    scripts/acceptance/arcade-flow.sh fbneo
  python3 scripts/acceptance/seed-arcade-schema-v2-launch.py "$temporary_root/data/retrom.db" fbneo
fi

netplay_nes_result="$temporary_root/netplay-nes.json"
netplay_fbneo_result="$temporary_root/netplay-fbneo.json"
if [[ "$case_id" =~ ^ACC-NP-01[456]$ ]]; then
  RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
  RETROM_ACCEPTANCE_BACKEND="$backend_origin" \
  RETROM_ACCEPTANCE_RESULT_FILE="$netplay_nes_result" \
    scripts/acceptance/netplay-nes-flow.sh
  go run scripts/acceptance/seed-public-arcade-dat.go \
    --database "$temporary_root/data/retrom.db" --fixture fbneo
  RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
  RETROM_ACCEPTANCE_BACKEND="$backend_origin" \
  RETROM_ACCEPTANCE_RESULT_FILE="$netplay_fbneo_result" \
    scripts/acceptance/arcade-flow.sh fbneo
fi

netplay_expansion_result="$temporary_root/netplay-expansion.json"
if [[ "$case_id" =~ ^ACC-NP-(01[789]|02[012])$ ]]; then
  case "$case_id" in
    ACC-NP-017) fixture_id="snes9x"; profile_id="snes9x-423-v1" ;;
    ACC-NP-018) fixture_id="nestopia"; profile_id="nestopia-423-v1" ;;
    ACC-NP-019) fixture_id="mame2003"; profile_id="mame2003-423-override-v1" ;;
    ACC-NP-020) fixture_id="mame2003_plus"; profile_id="mame2003-plus-423-v1" ;;
    ACC-NP-021) fixture_id="fbalpha2012_cps1"; profile_id="fbalpha2012-cps1-423-v1" ;;
    ACC-NP-022) fixture_id="fbalpha2012_cps2"; profile_id="fbalpha2012-cps2-423-v1" ;;
  esac
  if [[ "$fixture_id" == "snes9x" || "$fixture_id" == "nestopia" ]]; then
    RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
    RETROM_ACCEPTANCE_BACKEND="$backend_origin" \
    RETROM_ACCEPTANCE_RESULT_FILE="$netplay_expansion_result.source" \
      scripts/acceptance/netplay-nes-flow.sh "$fixture_id"
  else
    go run scripts/acceptance/seed-public-arcade-dat.go \
      --database "$temporary_root/data/retrom.db" --fixture "$fixture_id"
    RETROM_ACCEPTANCE_ORIGIN="$web_origin" \
    RETROM_ACCEPTANCE_BACKEND="$backend_origin" \
    RETROM_ACCEPTANCE_RESULT_FILE="$netplay_expansion_result.source" \
      scripts/acceptance/arcade-flow.sh "$fixture_id"
  fi
  jq -c --arg caseId "$case_id" --arg profileId "$profile_id" \
    '. + {caseId:$caseId,profileId:$profileId}' \
    "$netplay_expansion_result.source" >"$netplay_expansion_result"
fi

if [[ "$case_id" == "ACC-BIOS-007" ]]; then
  python3 scripts/acceptance/seed-bios-catalog.py "$temporary_root/data/retrom.db" 286
fi

if [[ "$case_id" == "ACC-UI-008" || "$case_id" == "ACC-UI-010" ]]; then
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
if [[ "$case_id" == "ACC-TAG-005" ]]; then
  specification="e2e/tags.spec.ts"
fi
if [[ "$case_id" == "ACC-STOR-001" ]]; then
  specification="e2e/storage-analysis.spec.ts"
fi
if [[ "$case_id" == "ACC-BIOS-006" || "$case_id" == "ACC-BIOS-007" || "$case_id" == "ACC-PEG-005" || "$case_id" == "ACC-PEG-006" || "$case_id" == "ACC-MEDIA-001" ]]; then
  specification="e2e/server-import.spec.ts"
fi
if [[ "$case_id" == "ACC-ES-005" || "$case_id" == "ACC-ES-006" ]]; then
  specification="e2e/emulationstation-import.spec.ts"
fi
if [[ "$case_id" =~ ^ACC-IMM-00[1-8]$ ]]; then
  specification="e2e/immersive.spec.ts"
fi
if [[ "$case_id" =~ ^ACC-NP-(01[456789]|02[012])$ ]]; then
  specification="e2e/netplay.spec.ts"
fi
playwright_grep="$case_id"
if [[ "$case_id" == "ACC-UI-010" ]]; then
  # ACC-UI-008 performs the stateful draft/decision setup consumed by 010.
  playwright_grep="ACC-UI-008|ACC-UI-010"
fi
playwright_args=(playwright test "$specification" --grep "$playwright_grep")
if [[ "$case_id" != "ACC-UI-005" && "$case_id" != "ACC-UI-006" && "$case_id" != "ACC-UI-009" && "$case_id" != "ACC-FAV-004" && "$case_id" != "ACC-BIOS-006" && "$case_id" != "ACC-PEG-005" && "$case_id" != "ACC-ES-005" && "$case_id" != "ACC-IMM-007" && "$case_id" != "ACC-MEDIA-001" && "$case_id" != "ACC-STOR-001" ]]; then
  playwright_args+=(--project=chrome-1280)
else
  playwright_args+=(--workers=1)
fi
core_expansion_results='[]'
if [[ -f "$core_expansion_result" ]]; then
  core_expansion_results="$(jq -sc '.' "$core_expansion_result")"
fi
netplay_expansion_results='[]'
if [[ -f "$netplay_expansion_result" ]]; then
  netplay_expansion_results="$(jq -sc '.' "$netplay_expansion_result")"
fi
(cd web && \
  RETROM_WEB_ORIGIN="$web_origin" \
  RETROM_E2E_DATABASE="$temporary_root/data/retrom.db" \
  RETROM_NETPLAY_NES_GAME_ID="$(jq -r '.gameId // empty' "$netplay_nes_result" 2>/dev/null || true)" \
  RETROM_NETPLAY_NES_FIXTURE_SHA256="$(jq -r '.fixtureSha256 // empty' "$netplay_nes_result" 2>/dev/null || true)" \
  RETROM_NETPLAY_FBNEO_GAME_ID="$(jq -r '.gameId // empty' "$netplay_fbneo_result" 2>/dev/null || true)" \
  RETROM_NETPLAY_FBNEO_FIXTURE_SHA256="$(jq -r '.fixtureSha256 // empty' "$netplay_fbneo_result" 2>/dev/null || true)" \
  RETROM_MAME2003_PLATFORM_INSTANCE_ID="$(jq -r '.platformInstanceId // empty' "$temporary_root/mame2003.json" 2>/dev/null || true)" \
  RETROM_CORE_EXPANSION_RESULTS="$core_expansion_results" \
  RETROM_NETPLAY_EXPANSION_RESULTS="$netplay_expansion_results" \
  RETROM_ACCEPTANCE_CASE_DIR="${RETROM_ACCEPTANCE_CASE_DIR:-}" \
  npm exec -- "${playwright_args[@]}")

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
