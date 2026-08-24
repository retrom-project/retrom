#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
evidence_root="$repository_root/.cache/retrom/acceptance"
mkdir -p "$evidence_root"
evidence="$(mktemp -d "$evidence_root/http-flow-XXXXXX")"
backend="${RETROM_ACCEPTANCE_BACKEND:-http://127.0.0.1:8080}"
origin="${RETROM_ACCEPTANCE_ORIGIN:-http://localhost:3000}"
fixture="$repository_root/testdata/public-roms/gba-smoke/gba-smoke.gba"
expected_size=1024
expected_sha256="f86c63b35aea59190f5e1cf99f8f3d576c3646b26da02f3f826fde192a47239b"
[[ -f "$fixture" ]] || { echo "public GBA smoke ROM is missing; run make public-fixtures-generate" >&2; exit 1; }
size="$(stat -c %s "$fixture")"
sha256="$(openssl dgst -sha256 "$fixture" | awk '{print $2}')"
[[ "$size" == "$expected_size" && "$sha256" == "$expected_sha256" ]] || {
  echo "PUBLIC_GBA_SMOKE_FIXTURE_DRIFT:size=$size:sha256=$sha256" >&2
  exit 1
}
digest="$(openssl dgst -sha256 -binary "$fixture" | base64 -w0)"

new_id() { python3 -c 'import uuid; print(uuid.uuid4())'; }

common=(-b "$evidence/cookies" -c "$evidence/cookies")
login="$(curl --fail --silent --show-error "${common[@]}" -H "Origin: $origin" -H "Content-Type: application/json" -d '{"username":"test","password":"test"}' "$backend/api/v1/auth/login")"
csrf="$(jq -r .csrfToken <<<"$login")"
write=(-H "Origin: $origin" -H "X-Retrom-Csrf: $csrf")

recommended="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" \
  -H "Content-Type: application/json" -H "Idempotency-Key: $(new_id)" -d '{}' \
  "$backend/api/v1/admin/platform-instances/recommendations/apply")"
platform_instance_id="$(jq -r '.created[] | select(.platformId=="gba" and .defaultCoreId=="mgba") | .id' <<<"$recommended")"
if [[ -z "$platform_instance_id" ]]; then
  directories="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/platform-instances")"
  platform_instance_id="$(jq -r '.items[] | select(.platformId=="gba" and .defaultCoreId=="mgba") | .id' <<<"$directories")"
fi
[[ -n "$platform_instance_id" ]] || { echo "recommended GBA directory was not created" >&2; exit 1; }

upload_body="$(jq -nc --argjson size "$size" '{sourceType:"FILES",files:[{clientFileId:"gba",relativePath:"Sudoku.gba",sizeBytes:$size}]}')"
upload="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: $(new_id)" -d "$upload_body" "$backend/api/v1/admin/uploads")"
upload_id="$(jq -r .uploadId <<<"$upload")"
file_id="$(jq -r '.files[0].fileId' <<<"$upload")"
curl --fail --silent --show-error "${common[@]}" "${write[@]}" -X PUT -H "Content-Type: application/octet-stream" -H "Content-Range: bytes 0-$((size - 1))/$size" -H "Content-Digest: sha-256=:$digest:" --data-binary "@$fixture" "$backend/api/v1/admin/uploads/$upload_id/files/$file_id/parts/0" -o /dev/null

curl --fail --silent --show-error "${common[@]}" -D "$evidence/upload-headers" -o "$evidence/upload.json" "$backend/api/v1/admin/uploads/$upload_id"
etag="$(awk 'tolower($1) == "etag:" {gsub("\r",""); print $2}' "$evidence/upload-headers")"
complete="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -X POST -H "If-Match: $etag" -H "Idempotency-Key: $(new_id)" "$backend/api/v1/admin/uploads/$upload_id/complete")"
job_id="$(jq -r .jobId <<<"$complete")"
state="QUEUED"
for _ in $(seq 1 100); do
  job="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/jobs/$job_id")"
  state="$(jq -r .state <<<"$job")"
  [[ "$state" == "SUCCEEDED" ]] && break
  [[ "$state" == "FAILED" || "$state" == "CANCELLED" ]] && { jq . <<<"$job" >&2; exit 1; }
  sleep 0.1
done
[[ "$state" == "SUCCEEDED" ]]

import_body="$(jq -nc --arg upload "$upload_id" --arg platformInstance "$platform_instance_id" '{uploadId:$upload,targetPlatformInstanceId:$platformInstance,metadataProvider:"NONE",tagIds:[]}')"
imported="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: $(new_id)" -d "$import_body" "$backend/api/v1/admin/imports")"
import_id="$(jq -r .importJobId <<<"$imported")"
reviews="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/reviews?importJobId=$import_id")"
item_id="$(jq -r '.items[0].itemId' <<<"$reviews")"
approved="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -X POST -H 'Content-Type: application/json' -H 'If-Match: "v1"' -H "Idempotency-Key: $(new_id)" -d '{"reason":null}' "$backend/api/v1/admin/reviews/$item_id/approve")"
game_id="$(jq -r .gameId <<<"$approved")"

attach_game_asset() {
  local asset_path="$1"
  local relative_path="$2"
  local kind="$3"
  local asset_size asset_digest asset_upload asset_upload_id asset_file_id asset_etag asset_job asset_state game_etag
  asset_size="$(stat -c %s "$asset_path")"
  asset_digest="$(openssl dgst -sha256 -binary "$asset_path" | base64 -w0)"
  asset_upload="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" \
    -H 'Content-Type: application/json' -H "Idempotency-Key: $(new_id)" \
    -d "$(jq -nc --arg path "$relative_path" --argjson size "$asset_size" '{sourceType:"FILES",files:[{clientFileId:"asset",relativePath:$path,sizeBytes:$size}]}')" \
    "$backend/api/v1/admin/uploads")"
  asset_upload_id="$(jq -r .uploadId <<<"$asset_upload")"
  asset_file_id="$(jq -r '.files[0].fileId' <<<"$asset_upload")"
  curl --fail --silent --show-error "${common[@]}" "${write[@]}" -X PUT \
    -H 'Content-Type: application/octet-stream' \
    -H "Content-Range: bytes 0-$((asset_size - 1))/$asset_size" \
    -H "Content-Digest: sha-256=:$asset_digest:" \
    --data-binary "@$asset_path" \
    "$backend/api/v1/admin/uploads/$asset_upload_id/files/$asset_file_id/parts/0" -o /dev/null
  curl --fail --silent --show-error "${common[@]}" -D "$evidence/asset-upload-headers" -o /dev/null \
    "$backend/api/v1/admin/uploads/$asset_upload_id"
  asset_etag="$(awk 'tolower($1) == "etag:" {gsub("\r",""); print $2}' "$evidence/asset-upload-headers")"
  asset_job="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -X POST \
    -H "If-Match: $asset_etag" -H "Idempotency-Key: $(new_id)" \
    "$backend/api/v1/admin/uploads/$asset_upload_id/complete")"
  asset_state="QUEUED"
  for _ in $(seq 1 100); do
    asset_state="$(curl --fail --silent --show-error "${common[@]}" \
      "$backend/api/v1/admin/jobs/$(jq -r .jobId <<<"$asset_job")" | jq -r .state)"
    [[ "$asset_state" == "SUCCEEDED" ]] && break
    [[ "$asset_state" == "FAILED" || "$asset_state" == "CANCELLED" ]] && return 1
    sleep 0.1
  done
  [[ "$asset_state" == "SUCCEEDED" ]]
  curl --fail --silent --show-error "${common[@]}" -D "$evidence/game-headers" -o /dev/null \
    "$backend/api/v1/admin/games/$game_id"
  game_etag="$(awk 'tolower($1) == "etag:" {gsub("\r",""); print $2}' "$evidence/game-headers")"
  curl --fail --silent --show-error "${common[@]}" "${write[@]}" \
    -H 'Content-Type: application/json' -H "If-Match: $game_etag" -H "Idempotency-Key: $(new_id)" \
    -d "$(jq -nc --arg file "$asset_file_id" --arg kind "$kind" '{uploadFileId:$file,kind:$kind,ordinal:0}')" \
    "$backend/api/v1/admin/games/$game_id/assets" -o /dev/null
}

attach_game_asset \
  "$repository_root/testdata/public-roms/gba-smoke/gba-smoke-cover.png" \
  "sudoku-cover.png" COVER
attach_game_asset \
  "$repository_root/testdata/public-roms/gba-smoke/gba-smoke-video.webm" \
  "sudoku-video.webm" VIDEO

launch_body="$(jq -nc --arg game "$game_id" '{gameId:$game,coreId:null,saveStateId:null,dosEntry:null,returnTo:("/games/"+$game),clientCapabilities:{secureContext:true,crossOriginIsolated:true,sharedArrayBuffer:true}}')"
launch="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: $(new_id)" -d "$launch_body" "$backend/api/v1/launches")"
launch_id="$(jq -r .launchId <<<"$launch")"
configuration="$(curl --fail --silent --show-error -b "$evidence/cookies" "$backend/runtime/launches/$launch_id/config")"
game_url="$(jq -r .gameUrl <<<"$configuration")"
curl --fail --silent --show-error -b "$evidence/cookies" -H "Range: bytes=0-31" -D "$evidence/range-headers" "$backend$game_url" -o "$evidence/range.bin"
[[ "$(stat -c %s "$evidence/range.bin")" == "32" ]]

jq -n \
  --arg uploadId "$upload_id" --arg importJobId "$import_id" --arg gameId "$game_id" --arg launchId "$launch_id" \
  --arg core "$(jq -r .core <<<"$configuration")" --arg adapter "$(jq -r .playerAdapterId <<<"$configuration")" \
  --arg rangeStatus "$(head -1 "$evidence/range-headers" | tr -d '\r')" --arg evidence "$evidence" \
  '{status:"PASSED",uploadId:$uploadId,importJobId:$importJobId,gameId:$gameId,launchId:$launchId,core:$core,playerAdapterId:$adapter,rangeStatus:$rangeStatus,evidenceDirectory:$evidence}' | tee "$evidence/result.json"
