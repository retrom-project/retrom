#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
exec "$root/.cache/tools/postgres-python/bin/python3" "$root/scripts/acceptance/browser.py" "$@"
