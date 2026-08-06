#!/usr/bin/env bash
set -euo pipefail

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)

if [[ ! -f "$repo_root/Makefile" ]]; then
  printf 'Makefile is not implemented yet; follow docs/dependency-management.md when scaffolding it.\n' >&2
  exit 2
fi

printf 'fetch-runtime.sh is a compatibility wrapper; the manifest-backed command is make prepare-deps.\n' >&2
exec make -C "$repo_root" prepare-deps
