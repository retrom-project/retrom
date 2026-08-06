#!/usr/bin/env bash
set -euo pipefail

kind="$1"
image="$2"
versions="$3"
active="$4"
docker_bin="$5"

before="$(python3 scripts/release-input-digest.py --versions "$versions" --active "$active")"
case "$kind" in
  backend)
    "$docker_bin" build --build-arg RETROM_DEPENDENCY_VERSIONS="$versions" \
      --build-arg RELEASE_INPUT_DIGEST="$before" \
      --label "io.retrom.release-input-sha256=$before" -t "$image" .
    ;;
  web)
    "$docker_bin" build --build-arg RELEASE_INPUT_DIGEST="$before" \
      --label "io.retrom.release-input-sha256=$before" -t "$image" web
    ;;
  *)
    echo "unknown image kind: $kind" >&2
    exit 2
    ;;
esac
after="$(python3 scripts/release-input-digest.py --versions "$versions" --active "$active")"
[[ "$before" == "$after" ]]
label="$($docker_bin image inspect --format '{{ index .Config.Labels "io.retrom.release-input-sha256" }}' "$image")"
[[ "$label" == "$before" ]]
