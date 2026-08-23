#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
target_directory="${1:-}"
fixture_root="$repository_root/testdata/public-roms/gba-smoke"
rom="$fixture_root/emulationstation-smoke.gba"
gamelist="$fixture_root/gamelist.xml"
cover="$fixture_root/emulationstation-smoke-cover.png"
video="$fixture_root/emulationstation-smoke-video.webm"
expected_rom_size=1024
expected_rom_sha256="b2e50f15541e172933fd1f0d02355105233f5e36b55d121c07f39079f21347c5"
expected_gamelist_size=478
expected_gamelist_sha256="f58df21608d161b9d3d53bba57fa2744658d66ae603026265b01686a685db50c"
expected_cover_size=20746
expected_cover_sha256="0d72b89ed87fcf349a3422d7f3888183ce57a3fa757bc6baab0365a70f7ccc02"
expected_video_size=767
expected_video_sha256="39a3044ce78c029049bda10b617724203bb91f4e2cb32ec5f15e3bdd45f6d10d"

if [[ -z "$target_directory" ]]; then
  echo "usage: prepare-emulationstation-gba-source.sh TARGET_DIRECTORY" >&2
  exit 2
fi

verify_fixture() {
  local path="$1"
  local expected_size="$2"
  local expected_sha256="$3"
  local label="$4"
  if [[ ! -f "$path" ]]; then
    echo "public EmulationStation $label fixture is missing; run make public-fixtures-generate" >&2
    exit 1
  fi
  local size
  local sha256
  size="$(stat -c %s "$path")"
  sha256="$(openssl dgst -sha256 "$path" | awk '{print $2}')"
  if [[ "$size" != "$expected_size" || "$sha256" != "$expected_sha256" ]]; then
    echo "PUBLIC_EMULATIONSTATION_GBA_SMOKE_FIXTURE_DRIFT:kind=$label:size=$size:sha256=$sha256" >&2
    exit 1
  fi
}

verify_fixture "$rom" "$expected_rom_size" "$expected_rom_sha256" rom
verify_fixture "$gamelist" "$expected_gamelist_size" "$expected_gamelist_sha256" gamelist
verify_fixture "$cover" "$expected_cover_size" "$expected_cover_sha256" cover
verify_fixture "$video" "$expected_video_size" "$expected_video_sha256" video

mkdir -p "$target_directory"
cp -p "$rom" "$target_directory/emulationstation-smoke.gba"
cp -p "$gamelist" "$target_directory/gamelist.xml"
cp -p "$cover" "$target_directory/emulationstation-smoke-cover.png"
cp -p "$video" "$target_directory/emulationstation-smoke-video.webm"
chmod a-w \
  "$target_directory/emulationstation-smoke.gba" \
  "$target_directory/gamelist.xml" \
  "$target_directory/emulationstation-smoke-cover.png" \
  "$target_directory/emulationstation-smoke-video.webm"
printf 'emulationstation_gba_fixture=prepared rom_size=%s gamelist_size=%s cover_size=%s video_size=%s\n' \
  "$expected_rom_size" "$expected_gamelist_size" "$expected_cover_size" "$expected_video_size"
