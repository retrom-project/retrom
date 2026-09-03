#!/usr/bin/env bash
set -euo pipefail

case "${PFB_ID:-}" in
  [a-z0-9]* ) ;;
  * ) echo 'PFB_SPEC_INVALID:entrypoint-id' >&2; exit 2 ;;
esac

python3 -m scripts.pfb.cli entrypoint-check --root /workspace/retrom --pfb-id "$PFB_ID"

mkdir -p /pfb-data/home /pfb-data/data /pfb-data/dev-state /pfb-data/dependencies /pfb-data/providers/installed
for source in dat auth netplay runtime-target-bindings; do
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

provider_arguments=()
if [[ "${PFB_RUNTIME_MODE:-formal}" == "branch" ]]; then
  provider_arguments+=(
    RETROM_PROVIDER_CANDIDATE_ROOT=/workspace/retrom/.pfb/candidates/runtime
    RETROM_PROVIDER_INSTALLED_ROOT=/pfb-data/providers/installed
    RETROM_PROVIDER_ACTIVE_PATH=/pfb-data/providers/active.json
    RETROM_PROVIDER_SOURCE=candidate
  )
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

make dev GO_PREPARE_MODE=system NODE_PREPARE_MODE=system "${provider_arguments[@]}" &
children+=("$!")

set +e
wait -n "${children[@]}"
status=$?
set -e
shutdown
exit "$status"
