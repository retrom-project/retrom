#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
target_directory="${1:-}"
fixture="$repository_root/testdata/public-roms/gba-smoke/pegasus-smoke.gba"
expected_size=1024
expected_sha256="6550cc49ddd91337c7c44bc827e2e9305b91c811ef6b032e1ee35fa5884a2e3a"

if [[ -z "$target_directory" ]]; then
  echo "usage: prepare-pegasus-gba-source.sh TARGET_DIRECTORY" >&2
  exit 2
fi
if [[ ! -f "$fixture" ]]; then
  echo "public Pegasus GBA smoke ROM is missing; run make public-fixtures-generate" >&2
  exit 1
fi
size="$(stat -c %s "$fixture")"
sha256="$(openssl dgst -sha256 "$fixture" | awk '{print $2}')"
if [[ "$size" != "$expected_size" || "$sha256" != "$expected_sha256" ]]; then
  echo "PUBLIC_PEGASUS_GBA_SMOKE_FIXTURE_DRIFT:size=$size:sha256=$sha256" >&2
  exit 1
fi

mkdir -p "$target_directory"
printf '%s\n' \
  'collection: GBA Smoke' \
  'game: Pegasus GBA Smoke' \
  'description: Retrom project-owned Pegasus product E2E fixture' \
  'file: pegasus-smoke.gba' \
  >"$target_directory/metadata.pegasus.txt"
cp -p "$fixture" "$target_directory/pegasus-smoke.gba"
printf 'pegasus_gba_fixture=prepared size=%s sha256=%s\n' "$size" "$sha256"
