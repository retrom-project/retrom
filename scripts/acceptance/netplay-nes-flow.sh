#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
backend="${RETROM_ACCEPTANCE_BACKEND:-http://127.0.0.1:8080}"
origin="${RETROM_ACCEPTANCE_ORIGIN:-http://localhost:3000}"
fixture="$repository_root/testdata/public-roms/nes-smoke/nes-smoke.nes"
expected_size=24592
expected_sha256="6b5224f3227879472e19e4d419008d77e69296140205771fd2df8370f18a01f8"
evidence="$(mktemp -d "$repository_root/.cache/retrom/acceptance/netplay-nes-flow-XXXXXX")"

python3 "$repository_root/testdata/public-roms/nes-smoke/build.py" --check
size="$(stat -c %s "$fixture")"
sha256="$(openssl dgst -sha256 "$fixture" | awk '{print $2}')"
[[ "$size" == "$expected_size" && "$sha256" == "$expected_sha256" ]]
digest="$(openssl dgst -sha256 -binary "$fixture" | base64 -w0)"
new_id() { python3 -c 'import uuid; print(uuid.uuid4())'; }

common=(-b "$evidence/cookies" -c "$evidence/cookies")
login="$(curl --fail --silent --show-error "${common[@]}" -H "Origin: $origin" -H "Content-Type: application/json" -d '{"username":"test","password":"test"}' "$backend/api/v1/auth/login")"
csrf="$(jq -r .csrfToken <<<"$login")"
write=(-H "Origin: $origin" -H "X-Retrom-Csrf: $csrf")

upload_body="$(jq -nc --argjson size "$size" '{sourceType:"FILES",files:[{clientFileId:"nes-netplay",relativePath:"Retrom Netplay Smoke.nes",sizeBytes:$size}]}')"
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

import_body="$(jq -nc --arg upload "$upload_id" '{uploadId:$upload,targetPlatformInstanceId:"01980000-0000-7000-8000-000000000001",metadataProvider:"NONE",tagIds:[]}')"
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
approved="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -X POST -H 'Content-Type: application/json' -H 'If-Match: "v1"' -H "Idempotency-Key: $(new_id)" -d '{"reason":null}' "$backend/api/v1/admin/reviews/$item_id/approve")"
game_id="$(jq -r .gameId <<<"$approved")"
detail="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/games/$game_id")"
jq -e '.coreOptions[] | select(.coreId == "fceumm" and .status == "READY")' <<<"$detail" >/dev/null

result="$(jq -nc --arg gameId "$game_id" --arg importJobId "$import_id" --arg sha256 "$sha256" --arg evidenceDirectory "$evidence" '{status:"PASSED",gameId:$gameId,importJobId:$importJobId,fixtureSha256:$sha256,evidenceDirectory:$evidenceDirectory}')"
printf '%s\n' "$result" | tee "$evidence/result.json"
if [[ -n "${RETROM_ACCEPTANCE_RESULT_FILE:-}" ]]; then
  printf '%s\n' "$result" >"$RETROM_ACCEPTANCE_RESULT_FILE"
fi
