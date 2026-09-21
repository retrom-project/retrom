#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PATH="$repository_root/.cache/tools/node-v24.18.0-linux-x64/bin:$PATH"
export PATH
temporary_root="$(mktemp -d "${TMPDIR:-/tmp}/retrom-network-acceptance.XXXXXX")"
backend_port=18081
web_port=13001
backend_origin="http://127.0.0.1:${backend_port}"
web_origin="http://localhost:${web_port}"
process_id=""

cleanup() {
  if [[ -n "$process_id" ]]; then
    kill -TERM -- "-$process_id" 2>/dev/null || true
    wait "$process_id" 2>/dev/null || true
  fi
  rm -rf -- "$temporary_root"
}
trap cleanup EXIT

wait_http() {
  local url="$1"
  local deadline=$((SECONDS + 90))
  until curl --fail --silent --show-error "$url" >/dev/null 2>&1; do
    if [[ -n "$process_id" ]] && ! kill -0 "$process_id" 2>/dev/null; then
      sed 's/^/[server] /' "$temporary_root/server.log" >&2
      echo "server exited before ${url} became available" >&2
      exit 1
    fi
    if (( SECONDS >= deadline )); then
      sed 's/^/[server] /' "$temporary_root/server.log" >&2
      echo "timed out waiting for ${url}" >&2
      exit 1
    fi
    sleep 0.2
  done
}

stop_group() {
  kill -INT -- "-$process_id" 2>/dev/null || true
  set +e
  wait "$process_id"
  set -e
  process_id=""
  local deadline=$((SECONDS + 5))
  while ss -ltn "sport = :${backend_port} or sport = :${web_port}" | tail -n +2 | grep -q .; do
    if (( SECONDS >= deadline )); then
      echo "network acceptance server left listeners behind" >&2
      exit 1
    fi
    sleep 0.1
  done
}

stop_dev() {
  "$repository_root/scripts/dev.sh" --stop 2>/dev/null || true
  set +e
  wait "$process_id"
  set -e
  process_id=""
  local deadline=$((SECONDS + 5))
  while ss -ltn "sport = :${backend_port} or sport = :${web_port}" | tail -n +2 | grep -q .; do
    if (( SECONDS >= deadline )); then
      echo "network acceptance dev server left listeners behind" >&2
      exit 1
    fi
    sleep 0.1
  done
}

for port in "$backend_port" "$web_port"; do
  if ss -ltn "sport = :${port}" | tail -n +2 | grep -q .; then
    echo "acceptance port is already in use: ${port}" >&2
    exit 1
  fi
done
mkdir -p "$temporary_root/data"
cd "$repository_root"

NEXT_BACKEND_ORIGIN="$backend_origin" make web-build
cat >"$temporary_root/start-production.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
backend_pid=""
web_pid=""
stop() {
  trap - INT TERM EXIT
  [[ -z "\$backend_pid" ]] || kill -TERM "\$backend_pid" 2>/dev/null || true
  [[ -z "\$web_pid" ]] || kill -TERM "\$web_pid" 2>/dev/null || true
  [[ -z "\$backend_pid" ]] || wait "\$backend_pid" 2>/dev/null || true
  [[ -z "\$web_pid" ]] || wait "\$web_pid" 2>/dev/null || true
}
trap stop INT TERM EXIT
go run ./cmd/retrom & backend_pid=\$!
(cd web && npm exec -- next start --hostname 127.0.0.1 --port ${web_port}) & web_pid=\$!
while kill -0 "\$backend_pid" 2>/dev/null && kill -0 "\$web_pid" 2>/dev/null; do
  wait -n "\$backend_pid" "\$web_pid" || status=\$?
  status=\${status:-0}
  if ! kill -0 "\$backend_pid" 2>/dev/null || ! kill -0 "\$web_pid" 2>/dev/null; then exit "\$status"; fi
done
EOF
chmod +x "$temporary_root/start-production.sh"
setsid env \
  RETROM_DATA_DIR="$temporary_root/data" \
  RETROM_HTTP_ADDR="127.0.0.1:${backend_port}" \
  RETROM_PUBLIC_ORIGIN="$web_origin" \
  RETROM_DEPENDENCY_ROOT="$repository_root/data" \
  RETROM_DEPENDENCY_VERSIONS="4.2.3" \
  RETROM_ACTIVE_EMULATORJS_VERSION="4.2.3" \
  NEXT_BACKEND_ORIGIN="$backend_origin" \
  "$temporary_root/start-production.sh" >"$temporary_root/server.log" 2>&1 &
process_id=$!
wait_http "$backend_origin/health/ready"
wait_http "$web_origin"
(cd web && \
  RETROM_WEB_ORIGIN="$web_origin" \
  RETROM_E2E_PRODUCTION=1 \
  RETROM_E2E_INTERNAL_ORIGIN="$backend_origin" \
  npm exec -- playwright test e2e/network.spec.ts --project=chrome-1280)
stop_group

: >"$temporary_root/server.log"
setsid make dev \
  RETROM_MODE="test" \
  RETROM_DATA_DIR="$temporary_root/data" \
  RETROM_HTTP_ADDR="127.0.0.1:${backend_port}" \
  RETROM_PUBLIC_ORIGIN="$web_origin" \
  NEXT_DEV_HOST="127.0.0.1" \
  NEXT_DEV_PORT="$web_port" \
  NEXT_BACKEND_ORIGIN="$backend_origin" \
  >"$temporary_root/server.log" 2>&1 &
process_id=$!
wait_http "$backend_origin/health/ready"
wait_http "$web_origin"
(cd web && \
  RETROM_WEB_ORIGIN="$web_origin" \
  RETROM_E2E_INTERNAL_ORIGIN="$backend_origin" \
  npm exec -- playwright test e2e/network.spec.ts --project=chrome-1280)
listeners="$(ss -ltnp "sport = :${backend_port} or sport = :${web_port}")"
printf '%s\n' "$listeners" | grep -q "127.0.0.1:${backend_port}"
printf '%s\n' "$listeners" | grep -q "127.0.0.1:${web_port}"
printf 'listeners:\n%s\n' "$listeners"
stop_dev

go test ./internal/httpapi -run '^TestWritesIgnoreBrowserOriginWithoutEnablingCORS$' -count=1
if rg -n --glob '*.go' --glob '*.ts' --glob '*.tsx' --glob 'Dockerfile' \
  'ListenAndServeTLS|tls\.Config|RETROM_TLS|certificate(_file)?|private_key' \
  cmd internal web Dockerfile web/Dockerfile; then
  echo "application source exposes an in-process TLS configuration path" >&2
  exit 1
fi
printf 'production_and_development_proxy_contract=ok\ntls_configuration_paths=0\n'
