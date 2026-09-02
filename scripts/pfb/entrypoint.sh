#!/usr/bin/env bash
set -euo pipefail

case "${PFB_ID:-}" in
  [a-z0-9]* ) ;;
  * ) echo 'PFB_SPEC_INVALID:entrypoint-id' >&2; exit 2 ;;
esac

python3 -m scripts.pfb.cli entrypoint-check --root /workspace/retrom --pfb-id "$PFB_ID"

mkdir -p /pfb-data/home /pfb-data/data /pfb-data/dev-state /pfb-data/dependencies
for source in dat auth netplay; do
  rm -rf "/pfb-data/dependencies/$source"
  cp -a "/workspace/retrom/data/$source" "/pfb-data/dependencies/$source"
done

export RETROM_PUBLIC_ORIGIN="http://${PFB_ID}.localhost:3000"
export RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN=true
export RETROM_PFB_ID="$PFB_ID"
export RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE="http://{launchId}.rpg.${PFB_ID}.localhost:3000"
export RETROM_HTTP_ADDR=0.0.0.0:8080
export RETROM_TRUSTED_PROXIES="${PFB_GATEWAY_IP}/32"
export RETROM_DEV_STATE_DIR=/pfb-data/dev-state
export RETROM_DATA_DIR=/pfb-data/data
export RETROM_DEPENDENCY_ROOT=/pfb-data/dependencies
export NEXT_DEV_HOST=0.0.0.0
export NEXT_DEV_PORT=3000
export NEXT_BACKEND_ORIGIN=http://127.0.0.1:8080
export NEXT_DEV_DIST_DIR=".next-pfb-${PFB_ID}"
export NODE_HOME=/usr/local

runtime_arguments=()
runtime_watch=false
if [[ "${PFB_RUNTIME_MODE:-formal}" == "branch" ]]; then
  (cd /workspace/runtime && npm ci)
  export RETROM_RUNTIME_DEV_RELEASE_OVERRIDES
  RETROM_RUNTIME_DEV_RELEASE_OVERRIDES="$(python3 -m scripts.pfb.cli runtime-overrides --root /workspace/retrom --pfb-id "$PFB_ID")"
  runtime_arguments+=(RETROM_RUNTIME_DEV_ROOT=/workspace/runtime RETROM_RUNTIME_DEV_INCLUDE_ASSETS=true RETROM_RUNTIME_PFB_CANDIDATE_ROOT=/workspace/retrom/.pfb/candidates/runtime)
  runtime_watch=true
fi

unset PFB_RUNTIME_MODE PFB_GATEWAY_IP

children=()
shutdown() {
  trap - TERM INT
  RETROM_DEV_STATE_DIR=/pfb-data/dev-state RETROM_DATA_DIR=/pfb-data/data \
    /workspace/retrom/scripts/dev.sh --stop 2>/dev/null || true
  if ((${#children[@]})); then
    kill -TERM "${children[@]}" 2>/dev/null || true
    wait "${children[@]}" 2>/dev/null || true
  fi
}
trap 'shutdown; exit 143' TERM INT

make dev GO_PREPARE_MODE=system NODE_PREPARE_MODE=system "${runtime_arguments[@]}" &
children+=("$!")

if [[ "$runtime_watch" == true ]]; then
  for _ in {1..300}; do
    kill -0 "${children[0]}" 2>/dev/null || { wait "${children[0]}"; exit $?; }
    if curl --fail --silent http://127.0.0.1:8080/health/live >/dev/null \
      && curl --fail --silent http://127.0.0.1:3000/ >/dev/null; then
      break
    fi
    sleep 1
  done
  curl --fail --silent http://127.0.0.1:8080/health/live >/dev/null \
    && curl --fail --silent http://127.0.0.1:3000/ >/dev/null \
    || { shutdown; echo 'PFB_UPSTREAM_UNAVAILABLE' >&2; exit 1; }
  (cd /workspace/runtime && npm run dev:watch) &
  children+=("$!")
fi

set +e
wait -n "${children[@]}"
status=$?
set -e
shutdown
exit "$status"
