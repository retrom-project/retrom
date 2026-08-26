#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fixture_id="${1:-mame2003}"
fixture_builder_root="$repository_root/testdata/public-roms/arcade-smoke"
dependency_mode="mame"
case "$fixture_id" in
  mame2003)
    core_id="mame2003"
    fixture_root="$fixture_builder_root"
    dat_path="$fixture_builder_root/mame2003-smoke.xml"
    game_archive="pacman.zip"
    ;;
  fbneo)
    core_id="fbneo"
    fixture_root="$fixture_builder_root/fbneo"
    dat_path="$fixture_root/fbneo-smoke.dat"
    game_archive="pacman.zip"
    ;;
  mame2003_plus)
    core_id="mame2003_plus"
    fixture_root="$fixture_builder_root/mame2003_plus"
    dat_path="$fixture_root/mame2003-plus-smoke.xml"
    game_archive="pacman.zip"
    ;;
  fbalpha2012_cps1)
    core_id="fbalpha2012_cps1"
    fixture_root="$fixture_builder_root/fbalpha2012_cps1"
    dat_path="$fixture_root/fbalpha2012-cps1-smoke.dat"
    game_archive="1941.zip"
    dependency_mode="none"
    ;;
  fbalpha2012_cps2)
    core_id="fbalpha2012_cps2"
    fixture_root="$fixture_builder_root/fbalpha2012_cps2"
    dat_path="$fixture_root/fbalpha2012-cps2-smoke.dat"
    game_archive="spf2xjd.zip"
    dependency_mode="cps2-parent"
    ;;
  *)
    echo "usage: arcade-flow.sh [mame2003|fbneo|mame2003_plus|fbalpha2012_cps1|fbalpha2012_cps2]" >&2
    exit 2
    ;;
esac
evidence_root="$repository_root/.cache/retrom/acceptance"
mkdir -p "$evidence_root"
evidence="$(mktemp -d "$evidence_root/arcade-${fixture_id}-flow-XXXXXX")"
backend="${RETROM_ACCEPTANCE_BACKEND:-http://127.0.0.1:8080}"
origin="${RETROM_ACCEPTANCE_ORIGIN:-http://localhost:3000}"

new_id() { python3 -c 'import uuid; print(uuid.uuid4())'; }

common=(-b "$evidence/cookies" -c "$evidence/cookies")
login="$(curl --fail --silent --show-error "${common[@]}" -H "Origin: $origin" -H "Content-Type: application/json" -d '{"username":"test","password":"test"}' "$backend/api/v1/auth/login")"
csrf="$(jq -r .csrfToken <<<"$login")"
write=(-H "Origin: $origin" -H "X-Retrom-Csrf: $csrf")

upload_files() {
  local response_name="$1"
  shift
  local files_json='[]'
  local index=0
  for path in "$@"; do
    local size
    size="$(stat -c %s "$path")"
    files_json="$(jq -c --arg id "file-$index" --arg path "$(basename "$path")" --argjson size "$size" '. + [{clientFileId:$id,relativePath:$path,sizeBytes:$size}]' <<<"$files_json")"
    index=$((index + 1))
  done
  local request_body
  request_body="$(jq -nc --argjson files "$files_json" '{sourceType:"FILES",files:$files}')"
  local upload
  upload="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: $(new_id)" -d "$request_body" "$backend/api/v1/admin/uploads")"
  local upload_id
  upload_id="$(jq -r .uploadId <<<"$upload")"
  local chunk_size
  chunk_size="$(jq -er .chunkSizeBytes <<<"$upload")"
  index=0
  for path in "$@"; do
    local file_id size offset part_number
    file_id="$(jq -r --argjson index "$index" '.files[$index].fileId' <<<"$upload")"
    size="$(stat -c %s "$path")"
    offset=0
    part_number=0
    while (( offset < size )); do
      local remaining part_size part_end chunk_path digest
      remaining=$((size - offset))
      part_size="$chunk_size"
      if (( remaining < chunk_size )); then
        part_size="$remaining"
      fi
      part_end=$((offset + part_size - 1))
      chunk_path="$evidence/$response_name-$index-$part_number.part"
      dd if="$path" of="$chunk_path" iflag=skip_bytes,count_bytes skip="$offset" count="$part_size" status=none
      digest="$(openssl dgst -sha256 -binary "$chunk_path" | base64 -w0)"
      curl --fail --silent --show-error "${common[@]}" "${write[@]}" -X PUT \
        -H "Content-Type: application/octet-stream" \
        -H "Content-Range: bytes $offset-$part_end/$size" \
        -H "Content-Digest: sha-256=:$digest:" \
        --data-binary "@$chunk_path" \
        "$backend/api/v1/admin/uploads/$upload_id/files/$file_id/parts/$part_number" -o /dev/null
      rm -f -- "$chunk_path"
      offset=$((part_end + 1))
      part_number=$((part_number + 1))
    done
    index=$((index + 1))
  done
  curl --fail --silent --show-error "${common[@]}" -D "$evidence/$response_name-upload-headers" -o "$evidence/$response_name-upload.json" "$backend/api/v1/admin/uploads/$upload_id"
  local etag complete job_id state
  etag="$(awk 'tolower($1) == "etag:" {gsub("\r",""); print $2}' "$evidence/$response_name-upload-headers")"
  complete="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -X POST -H "If-Match: $etag" -H "Idempotency-Key: $(new_id)" "$backend/api/v1/admin/uploads/$upload_id/complete")"
  job_id="$(jq -r .jobId <<<"$complete")"
  state="QUEUED"
  for _ in $(seq 1 200); do
    local job
    job="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/jobs/$job_id")"
    state="$(jq -r .state <<<"$job")"
    [[ "$state" == "SUCCEEDED" ]] && break
    [[ "$state" == "FAILED" || "$state" == "CANCELLED" ]] && { jq . <<<"$job" >&2; return 1; }
    sleep 0.1
  done
  [[ "$state" == "SUCCEEDED" ]]
  jq -nc --arg uploadId "$upload_id" --argjson files "$(jq -c .files <<<"$upload")" '{uploadId:$uploadId,files:$files}' >"$evidence/$response_name-result.json"
}

required_outputs=("$fixture_root/$game_archive" "$dat_path")
if [[ "$dependency_mode" == "mame" ]]; then
  required_outputs+=("$fixture_root/puckman.zip" "$fixture_root/retrombios.zip")
elif [[ "$dependency_mode" == "cps2-parent" ]]; then
  required_outputs+=("$fixture_root/spf2t.zip")
fi
for output in "${required_outputs[@]}"; do
  [[ -f "$output" ]] || {
    echo "public Arcade smoke fixture is missing; run make public-fixtures-generate" >&2
    exit 1
  }
done
python3 "$fixture_builder_root/build.py" --check
fixture_sha256="$(openssl dgst -sha256 "$fixture_root/$game_archive" | awk '{print $2}')"
printf 'arcade_flow=fixtures_verified\n'

core_artifacts="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/core-artifacts")"
core_artifact_id="$(jq -er --arg coreId "$core_id" '
  .items[]
  | select(
      .coreId == $coreId
      and .selectedForNewBindings == true
      and .availableForLaunch == true
    )
  | .id
' <<<"$core_artifacts")"
if [[ "$dependency_mode" == "mame" ]]; then
  upload_files bios "$fixture_root/retrombios.zip"
  bios_upload_file_id="$(jq -r '.files[0].fileId' "$evidence/bios-result.json")"
  bios_catalog="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/bios?scope=FULL_CATALOG&coreArtifactId=$core_artifact_id&q=retrombios.zip")"
  bios_requirement_id="$(jq -er '.items[] | select(.logicalName == "retrombios.zip") | .id' <<<"$bios_catalog")"
  bios_requirement_version="$(jq -er '.items[] | select(.logicalName == "retrombios.zip") | .version' <<<"$bios_catalog")"
  bios_installation="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -H "Content-Type: application/json" -H "If-Match: \"v$bios_requirement_version\"" -H "Idempotency-Key: $(new_id)" -d "$(jq -nc --arg uploadFileId "$bios_upload_file_id" '{uploadFileId:$uploadFileId}')" "$backend/api/v1/admin/bios/$bios_requirement_id/installations")"
  printf '%s\n' "$bios_installation" >"$evidence/bios-installation.json"
  [[ "$(jq -r .status <<<"$bios_installation")" == "MATCHED" ]]
  printf 'arcade_flow=bios_installed\n'
fi
platform_instances="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/platform-instances")"
printf '%s\n' "$platform_instances" >"$evidence/platform-instances.json"
platform_instance_id="$(jq -r --arg coreId "$core_id" '[.items[] | select(.defaultCoreId == $coreId) | .id][0] // empty' <<<"$platform_instances")"
if [[ -z "$platform_instance_id" ]]; then
  platform_instance="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" \
    -H "Content-Type: application/json" -H "Idempotency-Key: $(new_id)" \
    -d "$(jq -nc --arg coreId "$core_id" --arg name "$core_id acceptance games" \
      '{platformId:"arcade",defaultCoreId:$coreId,name:$name,description:"Acceptance-only core directory",sortOrder:10000}')" \
    "$backend/api/v1/admin/platform-instances")"
  platform_instance_id="$(jq -er .id <<<"$platform_instance")"
  printf '%s\n' "$platform_instance" >"$evidence/platform-instance-created.json"
fi
if [[ "$dependency_mode" == "mame" ]]; then
  upload_files arcade "$fixture_root/$game_archive" "$fixture_root/puckman.zip"
elif [[ "$dependency_mode" == "cps2-parent" ]]; then
  upload_files arcade "$fixture_root/$game_archive" "$fixture_root/spf2t.zip"
else
  upload_files arcade "$fixture_root/$game_archive"
fi
arcade_upload_id="$(jq -r .uploadId "$evidence/arcade-result.json")"
imported="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: $(new_id)" -d "$(jq -nc --arg uploadId "$arcade_upload_id" --arg target "$platform_instance_id" '{uploadId:$uploadId,targetPlatformInstanceId:$target,metadataProvider:"NONE",tagIds:[]}')" "$backend/api/v1/admin/imports")"
printf '%s\n' "$imported" >"$evidence/import.json"
import_id="$(jq -r .importJobId <<<"$imported")"
printf 'arcade_flow=import_created\n'

item_id=""
for _ in $(seq 1 200); do
  reviews="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/reviews?importJobId=$import_id")"
  printf '%s\n' "$reviews" >"$evidence/reviews.json"
  item_id="$(jq -r '.items[0].itemId // empty' <<<"$reviews")"
  [[ -n "$item_id" ]] && break
  sleep 0.1
done
[[ -n "$item_id" ]]
[[ "$(jq -r '.items | length' <<<"$reviews")" == "1" ]]
[[ "$(jq -r '.items[0].validationStatus' <<<"$reviews")" == "READY" ]]
review_detail="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/reviews/$item_id")"
printf '%s\n' "$review_detail" >"$evidence/review-detail.json"
dat_version_id="$(jq -er '.validation.dependencySnapshot.datVersionId' <<<"$review_detail")"
if [[ "$dependency_mode" == "mame" ]]; then
  jq -e --arg datVersionId "$dat_version_id" '
    .validation
    | select(.status == "READY" and .current == true)
    | .dependencySnapshot
    | select(.schemaVersion == 2 and .datVersionId == $datVersionId and (has("bios") | not))
    | ([.dependencies[] | select(.kind == "PARENT" and .machine == "puckman" and .state == "SATISFIED_EXTERNAL")] | length == 1)
      and ([.dependencies[] | select(.kind == "BIOS_OR_BASE" and .machine == "retrombios" and .state == "SATISFIED_EXTERNAL")] | length == 1)
  ' <<<"$review_detail" >/dev/null
elif [[ "$dependency_mode" == "cps2-parent" ]]; then
  jq -e --arg datVersionId "$dat_version_id" '
    .validation
    | select(.status == "READY" and .current == true)
    | .dependencySnapshot
    | select(.schemaVersion == 2 and .datVersionId == $datVersionId and (has("bios") | not))
    | ([.dependencies[] | select(.kind == "PARENT" and .machine == "spf2t" and .state == "SATISFIED_EXTERNAL")] | length == 1)
      and ([.dependencies[] | select(.kind == "BIOS_OR_BASE")] | length == 0)
  ' <<<"$review_detail" >/dev/null
else
  jq -e --arg datVersionId "$dat_version_id" '
    .validation
    | select(.status == "READY" and .current == true)
    | .dependencySnapshot
    | select(.schemaVersion == 2 and .datVersionId == $datVersionId and (has("bios") | not))
    | select(.dependencies | length == 0)
  ' <<<"$review_detail" >/dev/null
fi
printf 'arcade_flow=review_ready\n'
approval_body="$(jq -c '
  if (.duplicateGames | length) > 0
  then {reason:null,duplicatePolicy:"ALLOW_NEW",acknowledgedGameIds:[.duplicateGames[].gameId]}
  else {reason:null}
  end
' <<<"$review_detail")"
approved="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -X POST -H 'Content-Type: application/json' -H 'If-Match: "v1"' -H "Idempotency-Key: $(new_id)" -d "$approval_body" "$backend/api/v1/admin/reviews/$item_id/approve")"
printf '%s\n' "$approved" >"$evidence/approved.json"
game_id="$(jq -r .gameId <<<"$approved")"
printf 'arcade_flow=review_approved\n'

attach_acceptance_cover() {
  local cover_path="$1"
  local cover_upload_file_id game_etag
  [[ -f "$cover_path" ]] || {
    echo "acceptance cover is missing: $cover_path" >&2
    return 1
  }
  upload_files cover "$cover_path"
  cover_upload_file_id="$(jq -er '.files[0].fileId' "$evidence/cover-result.json")"
  curl --fail --silent --show-error "${common[@]}" -D "$evidence/game-headers" -o /dev/null \
    "$backend/api/v1/admin/games/$game_id"
  game_etag="$(awk 'tolower($1) == "etag:" {gsub("\r",""); print $2}' "$evidence/game-headers")"
  curl --fail --silent --show-error "${common[@]}" "${write[@]}" \
    -H 'Content-Type: application/json' -H "If-Match: $game_etag" -H "Idempotency-Key: $(new_id)" \
    -d "$(jq -nc --arg uploadFileId "$cover_upload_file_id" \
      '{uploadFileId:$uploadFileId,kind:"COVER",ordinal:0}')" \
    "$backend/api/v1/admin/games/$game_id/assets" -o /dev/null
  printf 'arcade_flow=cover_attached\n'
}

if [[ -n "${RETROM_ACCEPTANCE_COVER_PATH:-}" ]]; then
  attach_acceptance_cover "$RETROM_ACCEPTANCE_COVER_PATH"
fi

assert_game_detail_uses_arcade_snapshot() {
  local response_name="$1"
  local snapshot_family="$2"
  local game_detail admin_game
  game_detail="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/games/$game_id")"
  printf '%s\n' "$game_detail" >"$evidence/$response_name-game-detail.json"
  jq -e --arg coreId "$core_id" --arg datVersionId "$dat_version_id" '.coreOptions[] | select(.coreId == $coreId and .status == "READY" and .datVersionId == $datVersionId)' <<<"$game_detail" >/dev/null
  admin_game="$(curl --fail --silent --show-error "${common[@]}" "$backend/api/v1/admin/games/$game_id")"
  printf '%s\n' "$admin_game" >"$evidence/$response_name-admin-game.json"
  if [[ "$snapshot_family" == "ARCADE_V2" && "$dependency_mode" == "mame" ]]; then
    jq -e --arg coreId "$core_id" --arg datVersionId "$dat_version_id" '
      .variants[]
      | select(.coreId == $coreId)
      | .currentRevisionId as $current
      | .revisions[]
      | select(.id == $current and .status == "READY" and .datVersionId == $datVersionId)
      | .dependencySnapshot
      | select(.schemaVersion == 2 and .datVersionId == $datVersionId and (has("bios") | not))
      | ([.dependencies[] | select(.kind == "PARENT" and .machine == "puckman" and .state == "SATISFIED_EXTERNAL")] | length == 1)
        and ([.dependencies[] | select(.kind == "BIOS_OR_BASE" and .machine == "retrombios" and .state == "SATISFIED_EXTERNAL")] | length == 1)
    ' <<<"$admin_game" >/dev/null
    return
  fi
  if [[ "$snapshot_family" == "ARCADE_V2" && "$dependency_mode" == "cps2-parent" ]]; then
    jq -e --arg coreId "$core_id" --arg datVersionId "$dat_version_id" '
      .variants[]
      | select(.coreId == $coreId)
      | .currentRevisionId as $current
      | .revisions[]
      | select(.id == $current and .status == "READY" and .datVersionId == $datVersionId)
      | .dependencySnapshot
      | select(.schemaVersion == 2 and .datVersionId == $datVersionId and (has("bios") | not))
      | ([.dependencies[] | select(.kind == "PARENT" and .machine == "spf2t" and .state == "SATISFIED_EXTERNAL")] | length == 1)
        and ([.dependencies[] | select(.kind == "BIOS_OR_BASE")] | length == 0)
    ' <<<"$admin_game" >/dev/null
    return
  fi
  jq -e --arg coreId "$core_id" --arg datVersionId "$dat_version_id" '
    .variants[]
    | select(.coreId == $coreId)
    | .currentRevisionId as $current
    | .revisions[]
    | select(.id == $current and .status == "READY" and .datVersionId == $datVersionId)
    | .dependencySnapshot
    | select(.schemaVersion == 2 and .datVersionId == $datVersionId and (has("bios") | not))
    | select(.dependencies | length == 0)
  ' <<<"$admin_game" >/dev/null
}

assert_game_detail_uses_arcade_snapshot before-launch ARCADE_V2
printf 'arcade_flow=game_detail_uses_arcade_schema_v2\n'

launch_body="$(jq -nc --arg game "$game_id" --arg coreId "$core_id" '{gameId:$game,coreId:$coreId,saveStateId:null,dosEntry:null,returnTo:("/games/"+$game),clientCapabilities:{secureContext:true,crossOriginIsolated:true,sharedArrayBuffer:true}}')"
launch="$(curl --fail --silent --show-error "${common[@]}" "${write[@]}" -H "Content-Type: application/json" -H "Idempotency-Key: $(new_id)" -d "$launch_body" "$backend/api/v1/launches")"
initial_launch_status="$(jq -r '.status // "READY"' <<<"$launch")"
if [[ "$(jq -r .status <<<"$launch")" == "VALIDATION_PENDING" ]]; then
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
printf '%s\n' "$launch" >"$evidence/launch.json"
launch_id="$(jq -r .launchId <<<"$launch")"
[[ -n "$launch_id" && "$launch_id" != "null" ]]
printf 'arcade_flow=launch_created\n'
assert_game_detail_uses_arcade_snapshot after-launch ARCADE_V2
configuration="$(curl --fail --silent --show-error -b "$evidence/cookies" "$backend/runtime/launches/$launch_id/config")"
printf '%s\n' "$configuration" >"$evidence/configuration.json"
printf 'arcade_flow=config_loaded\n'
[[ "$(jq -r .runtimeCore <<<"$configuration")" == "$core_id" ]]
game_url="$(jq -er .gameUrl <<<"$configuration")"
curl --fail --silent --show-error -b "$evidence/cookies" "$backend$game_url" -o "$evidence/game.zip"
cmp "$fixture_root/$game_archive" "$evidence/game.zip"
if [[ "$dependency_mode" == "mame" ]]; then
  parent_url="$(jq -er .parentUrl <<<"$configuration")"
  bios_url="$(jq -er .biosUrl <<<"$configuration")"
  curl --fail --silent --show-error -b "$evidence/cookies" "$backend$parent_url" -o "$evidence/parent-bundle.zip"
  curl --fail --silent --show-error -b "$evidence/cookies" "$backend$bios_url" -o "$evidence/bios-bundle.zip"
  [[ "$(unzip -Z1 "$evidence/parent-bundle.zip")" == "puckman.zip" ]]
  [[ "$(unzip -Z1 "$evidence/bios-bundle.zip")" == "retrombios.zip" ]]
  unzip -p "$evidence/parent-bundle.zip" puckman.zip | cmp "$fixture_root/puckman.zip" -
  unzip -p "$evidence/bios-bundle.zip" retrombios.zip | cmp "$fixture_root/retrombios.zip" -
elif [[ "$dependency_mode" == "cps2-parent" ]]; then
  parent_url="$(jq -er .parentUrl <<<"$configuration")"
  jq -e 'select(.biosUrl == null)' <<<"$configuration" >/dev/null
  curl --fail --silent --show-error -b "$evidence/cookies" "$backend$parent_url" -o "$evidence/parent-bundle.zip"
  [[ "$(unzip -Z1 "$evidence/parent-bundle.zip")" == "spf2t.zip" ]]
  unzip -p "$evidence/parent-bundle.zip" spf2t.zip | cmp "$fixture_root/spf2t.zip" -
else
  jq -e 'select(.parentUrl == null and .biosUrl == null)' <<<"$configuration" >/dev/null
fi

result="$(jq -nc \
  --arg status "PASSED" --arg fixtureId "$fixture_id" --arg coreId "$core_id" --arg initialLaunchStatus "$initial_launch_status" \
  --arg datVersionId "$dat_version_id" --arg importJobId "$import_id" --arg platformInstanceId "$platform_instance_id" \
  --arg gameId "$game_id" --arg launchId "$launch_id" --arg fixtureSha256 "$fixture_sha256" --arg evidenceDirectory "$evidence" \
  '{status:$status,fixtureId:$fixtureId,coreId:$coreId,reviewDependencySnapshotSchemaVersion:2,initialLaunchStatus:$initialLaunchStatus,datVersionId:$datVersionId,importJobId:$importJobId,platformInstanceId:$platformInstanceId,gameId:$gameId,launchId:$launchId,fixtureSha256:$fixtureSha256,evidenceDirectory:$evidenceDirectory}' \
)"
printf '%s\n' "$result" | tee "$evidence/result.json"
if [[ -n "${RETROM_ACCEPTANCE_RESULT_FILE:-}" ]]; then
  printf '%s\n' "$result" >"$RETROM_ACCEPTANCE_RESULT_FILE"
fi
