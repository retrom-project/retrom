#!/usr/bin/env bash
set -euo pipefail
kind="$1"
image="$2"
docker_bin="$5"
if [[ -z "${RETROM_RUNTIME_TOOL_INPUT:-}" && -z "${RETROM_PROVIDER_INPUT:-}" ]]; then
  exec python3 scripts/prepare_image_inputs.py --run bash "$0" "$@"
fi
before="$(python3 scripts/image_input_digest.py --check)"
case "$kind" in
  backend)
    "$docker_bin" build \
      --build-context "runtime-tool=${RETROM_RUNTIME_TOOL_INPUT:?explicit tool input required}" \
      --build-context "providers=${RETROM_PROVIDER_INPUT:?explicit Provider input required}" \
      --build-context "runtime-dependencies=${RETROM_DEPENDENCY_ROOT:-$PWD/data}" \
      --build-arg IMAGE_INPUT_DIGEST="$before" -t "$image" .
    ;;
  web)
    "$docker_bin" build --build-arg RELEASE_INPUT_DIGEST="$before" \
      --build-arg "NEXT_BACKEND_ORIGIN=${NEXT_BACKEND_ORIGIN:-http://retrom:8080}" \
      --label "io.retrom.image-input-sha256=$before" -t "$image" web
    ;;
  *) echo 'unknown image kind' >&2; exit 2 ;;
esac
after="$(python3 scripts/image_input_digest.py)"
[[ "$before" == "$after" ]]
label="$($docker_bin image inspect --format '{{ index .Config.Labels "io.retrom.image-input-sha256" }}' "$image")"
[[ "$label" == "$before" ]]
