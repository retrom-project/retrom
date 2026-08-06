#!/usr/bin/env bash
set -euo pipefail

# Generated OpenAPI code is compared byte-for-byte by api-check and therefore
# retains the fixed generator's formatting.
mapfile -t files < <(find cmd internal migrations -name '*.go' -type f ! -path '*/generated/*' | sort)
if (( ${#files[@]} == 0 )); then
  exit 0
fi

status=0
for formatter in bin/gofumpt bin/goimports; do
  diff_output="$($formatter -d "${files[@]}")"
  if [[ -n "$diff_output" ]]; then
    printf '%s\n' "$diff_output" >&2
    status=1
  fi
done
exit "$status"
