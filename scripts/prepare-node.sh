#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
node_version="24.18.0"
node_directory="node-v${node_version}-linux-x64"
tools_directory="$repository_root/.cache/tools"
target="$tools_directory/$node_directory"

toolchain_is_valid() {
  local root="$1"
  [[ -x "$root/bin/node" && -x "$root/bin/npm" ]] || return 1
  [[ "$("$root/bin/node" --version 2>/dev/null)" == "v${node_version}" ]] || return 1
  [[ "$(PATH="$root/bin:$PATH" "$root/bin/npm" --version 2>/dev/null)" == "11.16.0" ]]
}

mkdir -p "$tools_directory"
exec 9>"$tools_directory/.prepare-node.lock"
flock -x 9
toolchain_is_valid "$target" && exit 0

temporary="$(mktemp -d "$tools_directory/.node-download-XXXXXX")"
restore_invalid=false
cleanup() {
  if [[ "$restore_invalid" == true && ! -e "$target" && -e "$temporary/invalid-toolchain" ]]; then
    mv -- "$temporary/invalid-toolchain" "$target"
  fi
  rm -rf -- "$temporary"
}
trap cleanup EXIT

if [[ -e "$target" || -L "$target" ]]; then
  echo "rebuilding invalid Node toolchain: $target" >&2
  mv -- "$target" "$temporary/invalid-toolchain"
  restore_invalid=true
fi

archive="${node_directory}.tar.xz"
curl --fail --location --silent --show-error "https://nodejs.org/dist/v${node_version}/SHASUMS256.txt" --output "$temporary/SHASUMS256.txt"
curl --fail --location --silent --show-error "https://nodejs.org/dist/v${node_version}/${archive}" --output "$temporary/$archive"
expected="$(awk -v archive="$archive" '$2 == archive { print $1 }' "$temporary/SHASUMS256.txt")"
if [[ ! "$expected" =~ ^[0-9a-f]{64}$ ]]; then
  echo "Node archive checksum is missing" >&2
  exit 1
fi
actual="$(sha256sum "$temporary/$archive" | awk '{print $1}')"
if [[ "$actual" != "$expected" ]]; then
  echo "Node archive checksum mismatch" >&2
  exit 1
fi
mkdir "$temporary/extracted"
tar -xJf "$temporary/$archive" -C "$temporary/extracted"
candidate="$temporary/extracted/$node_directory"
if ! toolchain_is_valid "$candidate"; then
  echo "downloaded Node toolchain is invalid" >&2
  exit 1
fi
mv -- "$candidate" "$target"
restore_invalid=false

toolchain_is_valid "$target"
