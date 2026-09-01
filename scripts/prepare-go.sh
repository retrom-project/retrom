#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
go_version="$(awk '$1 == "go" { print $2; exit }' "$repository_root/go.mod")"
go_archive="go${go_version}.linux-amd64.tar.gz"
go_directory="go${go_version}-linux-amd64"
tools_directory="$repository_root/.cache/tools"
target="$tools_directory/$go_directory"

case "$go_version" in
  1.26.5) expected_sha256="5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053" ;;
  *)
    echo "Go archive checksum is not pinned for version $go_version" >&2
    exit 1
    ;;
esac

toolchain_is_valid() {
  local root="$1"
  [[ -x "$root/bin/go" ]] || return 1
  [[ "$("$root/bin/go" env GOVERSION 2>/dev/null)" == "go${go_version}" ]]
}

mkdir -p "$tools_directory"
exec 9>"$tools_directory/.prepare-go.lock"
flock -x 9
toolchain_is_valid "$target" && exit 0

temporary="$(mktemp -d "$tools_directory/.go-download-XXXXXX")"
restore_invalid=false
cleanup() {
  if [[ "$restore_invalid" == true && ! -e "$target" && -e "$temporary/invalid-toolchain" ]]; then
    mv -- "$temporary/invalid-toolchain" "$target"
  fi
  rm -rf -- "$temporary"
}
trap cleanup EXIT

if [[ -e "$target" || -L "$target" ]]; then
  echo "rebuilding invalid Go toolchain: $target" >&2
  mv -- "$target" "$temporary/invalid-toolchain"
  restore_invalid=true
fi

curl --fail --location --silent --show-error \
  "https://go.dev/dl/$go_archive" \
  --output "$temporary/$go_archive"
actual_sha256="$(sha256sum "$temporary/$go_archive" | awk '{print $1}')"
if [[ "$actual_sha256" != "$expected_sha256" ]]; then
  echo "Go archive checksum mismatch" >&2
  exit 1
fi

mkdir "$temporary/extracted"
tar -xzf "$temporary/$go_archive" -C "$temporary/extracted"
candidate="$temporary/extracted/go"
if ! toolchain_is_valid "$candidate"; then
  echo "downloaded Go toolchain is invalid" >&2
  exit 1
fi
mv -- "$candidate" "$target"
restore_invalid=false

toolchain_is_valid "$target"
