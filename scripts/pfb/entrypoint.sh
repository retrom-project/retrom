#!/usr/bin/env bash
set -euo pipefail

case "${PFB_ID:-}" in
  [a-z0-9]* ) ;;
  * ) echo 'PFB_SPEC_INVALID:entrypoint-id' >&2; exit 2 ;;
esac

mkdir -p /pfb-workspace/{data,dev-state,home,providers/dev,providers/installed}

export RETROM_MODE=test
export RETROM_PUBLIC_ORIGIN="http://${PFB_ID}.localhost:3000"
export RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN=true
export RETROM_PFB_ID="$PFB_ID"
export RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE="http://{launchId}.rpg.${PFB_ID}.localhost:3000"
export RETROM_HTTP_ADDR=0.0.0.0:8080
export RETROM_TRUSTED_PROXIES="${PFB_GATEWAY_IP}/32"
export RETROM_DEV_STATE_DIR=/pfb-workspace/dev-state
export RETROM_DATA_DIR=/pfb-workspace/data
export RETROM_DEPENDENCY_ROOT=/workspace/retrom/data
export RETROM_DEPENDENCY_VERSIONS=4.2.3,4.3.0-pre
export RETROM_ACTIVE_EMULATORJS_VERSION=4.2.3
export RETROM_PROVIDER_INSTALLED_ROOT=/pfb-workspace/providers/installed
export RETROM_PROVIDER_ACTIVE_PATH=/pfb-workspace/providers/active.json
export RETROM_PROVIDER_DEV_ROOT=/pfb-workspace/providers/dev
export PFB_PROVIDER_INSTALLED_ROOT="$RETROM_PROVIDER_INSTALLED_ROOT"
export PFB_PROVIDER_ACTIVE_PATH="$RETROM_PROVIDER_ACTIVE_PATH"
export PFB_PROVIDER_DEV_ROOT="$RETROM_PROVIDER_DEV_ROOT"
export RETROM_MULTI_DISC_IMPORT_ENABLED=true
export RETROM_NETPLAY_ENABLED=true
export RETROM_NETPLAY_MAX_ACTIVE_ROOMS=16
export RETROM_SERVER_IMPORT_ROOTS='[]'
export NEXT_DEV_HOST=0.0.0.0
export NEXT_DEV_PORT=3000
export NEXT_BACKEND_ORIGIN=http://127.0.0.1:8080
export NEXT_DIST_DIR=".next-pfb-${PFB_ID}"
export NODE_HOME=/usr/local

unset PFB_GATEWAY_IP

node /workspace/runtime/scripts/pfb-provider-watch.mjs --once
node /workspace/runtime/scripts/pfb-provider-watch.mjs &
watcher_pid=$!
/workspace/retrom/scripts/dev.sh &
dev_pid=$!

shutdown() {
  trap - TERM INT
  RETROM_DEV_STATE_DIR=/pfb-workspace/dev-state RETROM_DATA_DIR=/pfb-workspace/data \
    /workspace/retrom/scripts/dev.sh --stop 2>/dev/null || true
  kill -TERM "$watcher_pid" "$dev_pid" 2>/dev/null || true
  wait "$watcher_pid" "$dev_pid" 2>/dev/null || true
}
trap 'shutdown; exit 143' TERM INT

set +e
wait -n "$watcher_pid" "$dev_pid"
status=$?
set -e
shutdown
exit "$status"
