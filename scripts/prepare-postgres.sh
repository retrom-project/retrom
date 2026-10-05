#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python3 "$repository_root/scripts/local_user.py"
version=18.3
expected_sha256=d95663fbbf3a80f81a9d98d895266bdcb74ba274bcc04ef6d76630a72dee016f
tools_directory="$repository_root/.cache/tools"
target="$tools_directory/postgresql-$version"

valid() {
  local root="$1" tool
  for tool in postgres initdb psql pg_isready; do
    [[ -x "$root/bin/$tool" ]] || return 1
    [[ "$("$root/bin/$tool" --version)" == "$tool (PostgreSQL) $version" ]] || return 1
  done
}

mkdir -p "$tools_directory"
exec 9>"$tools_directory/.prepare-postgres.lock"
flock -x 9
valid "$target" && exit 0
temporary="$(mktemp -d "$tools_directory/.postgres-build-XXXXXX")"
cleanup() {
  if [[ ! -e "$target" && -e "$temporary/previous" ]]; then
    mv -- "$temporary/previous" "$target"
  fi
  rm -rf -- "$temporary"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "Preparing native PostgreSQL $version (first build may take several minutes)" >&2
curl --fail --location --silent --show-error \
  "https://ftp.postgresql.org/pub/source/v$version/postgresql-$version.tar.bz2" \
  --output "$temporary/source.tar.bz2"
printf '%s  %s\n' "$expected_sha256" "$temporary/source.tar.bz2" | sha256sum --check --status
tar -xjf "$temporary/source.tar.bz2" -C "$temporary"
(
  cd "$temporary/postgresql-$version"
  # PostgreSQL generates prerequisite headers only at its own top-level Make.
  # Do not inherit Retrom's recursion depth, jobserver or command-line overrides.
  ./configure --prefix="$target" --without-icu --without-readline &&
    env -u MAKELEVEL -u MAKEFLAGS -u MFLAGS -u MAKEOVERRIDES make -j "$(nproc)" &&
    env -u MAKELEVEL -u MAKEFLAGS -u MFLAGS -u MAKEOVERRIDES make install DESTDIR="$temporary/stage"
) >"$temporary/build.log" 2>&1 || { cat "$temporary/build.log" >&2; exit 1; }
candidate="$temporary/stage$target"
valid "$candidate"
if [[ -e "$target" ]]; then mv -- "$target" "$temporary/previous"; fi
mv -- "$candidate" "$target"
valid "$target"
