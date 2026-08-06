#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
node_version="24.18.0"
node_directory="node-v${node_version}-linux-x64"
tools_directory="$repository_root/.cache/tools"
target="$tools_directory/$node_directory"

if [[ -x "$target/bin/node" ]] && [[ "$($target/bin/node --version)" == "v${node_version}" ]] && [[ "$($target/bin/npm --version)" == "11.16.0" ]]; then
  exit 0
fi
if [[ -e "$target" ]]; then
  echo "existing Node toolchain is invalid: $target" >&2
  exit 1
fi

mkdir -p "$tools_directory"
temporary="$(mktemp -d "$tools_directory/.node-download-XXXXXX")"
cleanup() { rm -rf -- "$temporary"; }
trap cleanup EXIT

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
tar -xJf "$temporary/$archive" -C "$tools_directory"

[[ "$($target/bin/node --version)" == "v${node_version}" ]]
[[ "$($target/bin/npm --version)" == "11.16.0" ]]
