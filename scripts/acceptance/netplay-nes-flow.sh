#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
backend="${RETROM_ACCEPTANCE_BACKEND:-http://127.0.0.1:8080}"
origin="${RETROM_ACCEPTANCE_ORIGIN:-http://localhost:4000}"
fixture_id="${1:-fceumm}"
case "$fixture_id" in
  fceumm)
    platform_id="nes"
    core_id="fceumm"
    fixture="$repository_root/testdata/public-roms/nes-smoke/nes-smoke.nes"
    fixture_builder="$repository_root/testdata/public-roms/nes-smoke/build.py"
    expected_size=24592
    expected_sha256="6b5224f3227879472e19e4d419008d77e69296140205771fd2df8370f18a01f8"
    logical_name="Retrom FCEUmm Netplay Smoke.nes"
    ;;
  nestopia)
    platform_id="nes"
    core_id="nestopia"
    fixture="$repository_root/testdata/public-roms/nes-smoke/nestopia-smoke.nes"
    fixture_builder="$repository_root/testdata/public-roms/nes-smoke/build.py"
    expected_size=24592
    expected_sha256="ab4adf02261946fbb80bb8a2141908589fd6cd7a32408875d7541eb94efc61ff"
    logical_name="Retrom Nestopia Netplay Smoke.nes"
    ;;
  snes9x)
    platform_id="snes"
    core_id="snes9x"
    fixture="$repository_root/testdata/public-roms/snes-smoke/snes-smoke.sfc"
    fixture_builder="$repository_root/testdata/public-roms/snes-smoke/build.py"
    expected_size=32768
    expected_sha256="408574e6a6b7db1273e21142789bc50e5a1acb529bcf61c059cced5cfe1082db"
    logical_name="Retrom SNES9x Netplay Smoke.sfc"
    ;;
  *)
    echo "usage: netplay-nes-flow.sh [fceumm|nestopia|snes9x]" >&2
    exit 2
    ;;
esac
evidence="$(mktemp -d "$repository_root/.cache/retrom/acceptance/netplay-${fixture_id}-flow-XXXXXX")"

python3 "$fixture_builder" --check
size="$(stat -c %s "$fixture")"
sha256="$(openssl dgst -sha256 "$fixture" | awk '{print $2}')"
[[ "$size" == "$expected_size" && "$sha256" == "$expected_sha256" ]]
digest="$(openssl dgst -sha256 -binary "$fixture" | base64 -w0)"
new_id() { python3 -c 'import uuid; print(uuid.uuid4())'; }

common=(-b "$evidence/cookies" -c "$evidence/cookies")
login="$(curl --fail --silent --show-error "${common[@]}" -H "Origin: $origin" -H "Content-Type: application/json" -d '{"username":"test","password":"test"}' "$backend/api/v1/auth/login")"
csrf="$(jq -r .csrfToken <<<"$login")"
write=(-H "Origin: $origin" -H "X-Retrom-Csrf: $csrf")

platform_instances="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/platform-instances")"
platform_instance_id="$(jq -r --arg platformId "$platform_id" --arg coreId "$core_id" '[.items[] | select(.platformId == $platformId and .defaultCoreId == $coreId) | .id][0] // empty' <<<"$platform_instances")"
if [[ -z "$platform_instance_id" ]]; then
  curl --fail --silent --show-error "${common[@]}" "${write[@]}" \
    -H "Content-Type: application/json" -H "Idempotency-Key: $(new_id)" \
    -d '{}' "$backend/api/v1/admin/platform-instances/recommendations/apply" >/dev/null
  platform_instances="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/platform-instances")"
  platform_instance_id="$(jq -r --arg platformId "$platform_id" --arg coreId "$core_id" '[.items[] | select(.platformId == $platformId and .defaultCoreId == $coreId) | .id][0] // empty' <<<"$platform_instances")"
fi
if [[ -z "$platform_instance_id" ]]; then
  platform_instance="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" \
    -H "Content-Type: application/json" -H "Idempotency-Key: $(new_id)" \
    -d "$(jq -nc --arg platformId "$platform_id" --arg coreId "$core_id" --arg name "$core_id acceptance games" '{platformId:$platformId,defaultCoreId:$coreId,name:$name,description:"Acceptance-only core directory",sortOrder:10000}')" \
    "$backend/api/v1/admin/platform-instances")"
  platform_instance_id="$(jq -er .id <<<"$platform_instance")"
fi

upload_body="$(jq -nc --argjson size "$size" --arg path "$logical_name" '{sourceType:"FILES",files:[{clientFileId:"netplay-fixture",relativePath:$path,sizeBytes:$size}]}')"
upload="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: $(new_id)" -d "$upload_body" "$backend/api/v1/admin/uploads")"
upload_id="$(jq -r .uploadId <<<"$upload")"
file_id="$(jq -r '.files[0].fileId' <<<"$upload")"
curl --fail --silent --show-error "${common[@]}" "${write[@]}" -X PUT \
  -H "Content-Type: application/octet-stream" \
  -H "Content-Range: bytes 0-$((size - 1))/$size" \
  -H "Content-Digest: sha-256=:$digest:" \
  --data-binary "@$fixture" \
  "$backend/api/v1/admin/uploads/$upload_id/files/$file_id/parts/0" -o /dev/null
curl --fail --silent --show-error "${common[@]}" -D "$evidence/upload-headers" -o "$evidence/upload.json" "$backend/api/v1/admin/uploads/$upload_id"
etag="$(awk 'tolower($1) == "etag:" {gsub("\r",""); print $2}' "$evidence/upload-headers")"
complete="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -X POST -H "If-Match: $etag" -H "Idempotency-Key: $(new_id)" "$backend/api/v1/admin/uploads/$upload_id/complete")"
job_id="$(jq -r .jobId <<<"$complete")"
state=QUEUED
for _ in $(seq 1 200); do
  job="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/jobs/$job_id")"
  state="$(jq -r .state <<<"$job")"
  [[ "$state" == SUCCEEDED ]] && break
  [[ "$state" == FAILED || "$state" == CANCELLED ]] && { jq . <<<"$job" >&2; exit 1; }
  sleep 0.1
done
[[ "$state" == SUCCEEDED ]]

import_body="$(jq -nc --arg upload "$upload_id" --arg target "$platform_instance_id" '{uploadId:$upload,targetPlatformInstanceId:$target,metadataProvider:"NONE",tagIds:[]}')"
imported="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: $(new_id)" -d "$import_body" "$backend/api/v1/admin/imports")"
import_id="$(jq -r .importJobId <<<"$imported")"
item_id=""
for _ in $(seq 1 200); do
  reviews="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/reviews?importJobId=$import_id")"
  item_id="$(jq -r '.items[0].itemId // empty' <<<"$reviews")"
  [[ -n "$item_id" ]] && break
  sleep 0.1
done
[[ -n "$item_id" ]]
[[ "$(jq -r '.items[0].validationStatus' <<<"$reviews")" == READY ]]
review_detail="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/reviews/$item_id")"
approval_body="$(jq -c '
  if (.duplicateGames | length) > 0
  then {reason:null,duplicatePolicy:"ALLOW_NEW",acknowledgedGameIds:[.duplicateGames[].gameId]}
  else {reason:null}
  end
' <<<"$review_detail")"
approved="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -X POST -H 'Content-Type: application/json' -H 'If-Match: "v1"' -H "Idempotency-Key: $(new_id)" -d "$approval_body" "$backend/api/v1/admin/reviews/$item_id/approve")"
game_id="$(jq -r .gameId <<<"$approved")"
detail="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/games/$game_id")"
jq -e --arg gameId "$game_id" 'select(.gameId == $gameId)' <<<"$detail" >/dev/null

launch_body="$(jq -nc --arg game "$game_id" --arg coreId "$core_id" '{gameId:$game,coreId:$coreId,saveStateId:null,dosEntry:null,returnTo:("/games/"+$game),clientCapabilities:{secureContext:true,crossOriginIsolated:true,sharedArrayBuffer:true}}')"
launch="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: $(new_id)" -d "$launch_body" "$backend/api/v1/launches")"
if [[ "$(jq -r '.status // "READY"' <<<"$launch")" == "VALIDATION_PENDING" ]]; then
  validation_job_id="$(jq -r .jobId <<<"$launch")"
  validation_state="QUEUED"
  for _ in $(seq 1 200); do
    validation_job="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/jobs/$validation_job_id")"
    validation_state="$(jq -r .state <<<"$validation_job")"
    [[ "$validation_state" == "SUCCEEDED" ]] && break
    [[ "$validation_state" == "FAILED" || "$validation_state" == "CANCELLED" ]] && { jq . <<<"$validation_job" >&2; exit 1; }
    sleep 0.1
  done
  [[ "$validation_state" == "SUCCEEDED" ]]
  launch="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: $(new_id)" -d "$launch_body" "$backend/api/v1/launches")"
fi
launch_id="$(jq -er .launchId <<<"$launch")"
configuration="$(curl --fail --silent --show-error -b "$evidence/cookies" "$backend/runtime/launches/$launch_id/config")"
jq -e --arg coreId "$core_id" 'select(.runtimeCore == $coreId and .biosUrl == null and .parentUrl == null)' <<<"$configuration" >/dev/null
game_url="$(jq -er .gameUrl <<<"$configuration")"
curl --fail --silent --show-error -b "$evidence/cookies" "$backend$game_url" -o "$evidence/game.rom"
cmp "$fixture" "$evidence/game.rom"
detail="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/games/$game_id")"
jq -e --arg coreId "$core_id" '.coreOptions[] | select(.coreId == $coreId and .status == "READY")' <<<"$detail" >/dev/null

result="$(jq -nc --arg fixtureId "$fixture_id" --arg coreId "$core_id" --arg platformInstanceId "$platform_instance_id" --arg gameId "$game_id" --arg launchId "$launch_id" --arg importJobId "$import_id" --arg sha256 "$sha256" --arg evidenceDirectory "$evidence" '{status:"PASSED",fixtureId:$fixtureId,coreId:$coreId,platformInstanceId:$platformInstanceId,gameId:$gameId,launchId:$launchId,importJobId:$importJobId,fixtureSha256:$sha256,evidenceDirectory:$evidenceDirectory}')"
printf '%s\n' "$result" | tee "$evidence/result.json"
if [[ -n "${RETROM_ACCEPTANCE_RESULT_FILE:-}" ]]; then
  printf '%s\n' "$result" >"$RETROM_ACCEPTANCE_RESULT_FILE"
fi
