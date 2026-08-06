#!/usr/bin/env bash
set -euo pipefail

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
manifest_path="$script_dir/fixtures.json"
source_host=${RETROM_FIXTURE_HOST:?Set RETROM_FIXTURE_HOST to the SSH host from your local inventory}
source_root=${RETROM_FIXTURE_ROOT:?Set RETROM_FIXTURE_ROOT to the absolute fixture root on that host}
source_root=${source_root%/}

if [[ "$source_host" == *$'\n'* || "$source_host" == -* ]]; then
  printf 'RETROM_FIXTURE_HOST contains an unsupported value\n' >&2
  exit 2
fi
if [[ "$source_root" != /* || "$source_root" == *$'\n'* ]]; then
  printf 'RETROM_FIXTURE_ROOT must be an absolute remote path without newlines\n' >&2
  exit 2
fi
command -v jq >/dev/null || { printf 'jq is required\n' >&2; exit 2; }
command -v unzip >/dev/null || { printf 'unzip is required\n' >&2; exit 2; }

umask 077
stage_dir=$(mktemp -d /tmp/retrom-fixtures.XXXXXX)
declare -a staged_files=()
declare -a target_files=()
declare -A queued_targets=()

cleanup() {
  rm -rf -- "$stage_dir"
}
trap cleanup EXIT

validate_relative() {
  local value=$1
  if [[ -z "$value" || "$value" == /* || "$value" == *$'\n'* || "$value" == *$'\t'* || "/$value/" == *"/../"* ]]; then
    printf 'Rejected unsafe relative path from fixture manifest\n' >&2
    exit 2
  fi
}

fetch_verified() {
  local relative_path=$1
  local expected_size=$2
  local expected_sha256=$3
  local staged_path="$stage_dir/download-$expected_sha256"
  validate_relative "$relative_path"
  if [[ ! -f "$staged_path" ]]; then
    scp -q -- "$source_host:$source_root/$relative_path" "$staged_path"
  fi
  local actual_size actual_sha256
  actual_size=$(stat -c '%s' "$staged_path")
  actual_sha256=$(sha256sum "$staged_path" | cut -d ' ' -f 1)
  if [[ "$actual_size" != "$expected_size" || "$actual_sha256" != "$expected_sha256" ]]; then
    printf 'Fixture digest mismatch for expected SHA-256 %s\n' "$expected_sha256" >&2
    exit 1
  fi
  printf '%s\n' "$staged_path"
}

queue_install() {
  local staged_path=$1
  local repo_relative_target=$2
  validate_relative "$repo_relative_target"
  if [[ "$repo_relative_target" != data/game/* ]]; then
    printf 'Fixture target must remain under data/game\n' >&2
    exit 2
  fi
  if [[ -n "${queued_targets[$repo_relative_target]:-}" ]]; then
    if [[ "${queued_targets[$repo_relative_target]}" != "$staged_path" ]]; then
      printf 'Conflicting fixture target in manifest\n' >&2
      exit 2
    fi
    return
  fi
  queued_targets[$repo_relative_target]=$staged_path
  staged_files+=("$staged_path")
  target_files+=("$repo_root/$repo_relative_target")
}

while IFS=$'\t' read -r kind source_relative source_size source_sha target target_size target_sha member extracted_target; do
  source_file=$(fetch_verified "$source_relative" "$source_size" "$source_sha")
  if [[ "$kind" == "DIRECT" ]]; then
    queue_install "$source_file" "$target"
    continue
  fi
  validate_relative "$member"
  extracted_file="$stage_dir/extracted-$target_sha"
  unzip -p "$source_file" "$member" > "$extracted_file"
  if [[ $(stat -c '%s' "$extracted_file") != "$target_size" || $(sha256sum "$extracted_file" | cut -d ' ' -f 1) != "$target_sha" ]]; then
    printf 'Extracted fixture digest mismatch for expected SHA-256 %s\n' "$target_sha" >&2
    exit 1
  fi
  queue_install "$source_file" "$target"
  queue_install "$extracted_file" "$extracted_target"
done < <(jq -r '
  .fixtures[].game |
  if has("sourceArchiveLocalPath") then
    ["ARCHIVE", .sourceRelativePath, .sourceArchiveSize, .sourceArchiveSha256,
     .sourceArchiveLocalPath, .size, .sha256, .extractedMember, .localPath]
  else
    ["DIRECT", .sourceRelativePath, .size, .sha256, .localPath, .size, .sha256, "-", "-"]
  end | @tsv
' "$manifest_path")

while IFS=$'\t' read -r source_relative size sha target; do
  source_file=$(fetch_verified "$source_relative" "$size" "$sha")
  queue_install "$source_file" "$target"
done < <(jq -r '.fixtures[].bios[] | [.sourceRelativePath, .size, .sha256, .localPath] | @tsv' "$manifest_path")

for index in "${!target_files[@]}"; do
  mkdir -p -- "$(dirname -- "${target_files[$index]}")"
  install -m 0644 -- "${staged_files[$index]}" "${target_files[$index]}"
done

printf 'Installed verified fixtures under %s/data/game\n' "$repo_root"
