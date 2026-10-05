#!/usr/bin/env bash
set -euo pipefail
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tools_root="$repository_root/.cache/tools/postgres-python"
if [[ ! -x "$tools_root/bin/python3" ]]; then
  python3 -m venv "$tools_root"
fi
requirements="$repository_root/scripts/acceptance/requirements.txt"
expected="$(sha256sum "$requirements" | cut -d ' ' -f 1)"
if [[ ! -f "$tools_root/requirements.sha256" || "$(cat "$tools_root/requirements.sha256")" != "$expected" ]]; then
  "$tools_root/bin/python3" -m pip install --disable-pip-version-check -r "$requirements"
  printf '%s\n' "$expected" > "$tools_root/requirements.sha256"
fi
