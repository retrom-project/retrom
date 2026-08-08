#!/usr/bin/env bash
set -euo pipefail

fixture_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
stage_dir=$(mktemp -d /tmp/retrom-sevenzip-fixtures.XXXXXX)

for archive in single ambiguous nested casefold symlink unsupported-coder; do
  rm -f -- "$fixture_dir/$archive.7z"
done
if [[ ${RETROM_REGENERATE_ENCRYPTED:-0} == 1 ]]; then
  rm -f -- "$fixture_dir/encrypted.7z"
fi

cleanup() {
  rm -rf -- "$stage_dir"
}
trap cleanup EXIT

install -m 0644 "$fixture_dir/payload/game.a26" "$stage_dir/game.a26"
(
  cd "$stage_dir"
  7z a -t7z -m0=lzma2 -mx=5 -mmt=off -mtm=off -mta=off -mtc=off "$fixture_dir/single.7z" game.a26
)

install -m 0644 "$fixture_dir/payload/second.a26" "$stage_dir/second.a26"
(
  cd "$stage_dir"
  7z a -t7z -m0=lzma2 -mx=5 -mmt=off -mtm=off -mta=off -mtc=off "$fixture_dir/ambiguous.7z" game.a26 second.a26
)

install -m 0644 "$fixture_dir/payload/nested.zip" "$stage_dir/nested.zip"
(
  cd "$stage_dir"
  7z a -t7z -m0=lzma2 -mx=5 -mmt=off -mtm=off -mta=off -mtc=off "$fixture_dir/nested.7z" nested.zip
  if [[ ! -f "$fixture_dir/encrypted.7z" ]]; then
    7z a -t7z -m0=lzma2 -mx=5 -mmt=off -mtm=off -mta=off -mtc=off -pfixture -mhe=on "$fixture_dir/encrypted.7z" game.a26
  fi
)

mkdir "$stage_dir/casefold"
install -m 0644 "$fixture_dir/payload/game.a26" "$stage_dir/casefold/ROM.A26"
install -m 0644 "$fixture_dir/payload/second.a26" "$stage_dir/casefold/rom.a26"
(
  cd "$stage_dir/casefold"
  7z a -t7z -m0=lzma2 -mx=5 -mmt=off -mtm=off -mta=off -mtc=off "$fixture_dir/casefold.7z" ROM.A26 rom.a26
)

ln -s game.a26 "$stage_dir/link.a26"
(
  cd "$stage_dir"
  7z a -t7z -m0=lzma2 -mx=5 -mmt=off -mtm=off -mta=off -mtc=off -snl "$fixture_dir/symlink.7z" link.a26
  7z a -t7z -m0=Deflate64 -mx=5 -mmt=off -mtm=off -mta=off -mtc=off "$fixture_dir/unsupported-coder.7z" game.a26
)

sha256sum "$fixture_dir"/*.7z
