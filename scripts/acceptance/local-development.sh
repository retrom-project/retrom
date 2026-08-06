#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
temporary_root="$(mktemp -d "${TMPDIR:-/tmp}/retrom-dev-acceptance.XXXXXX")"
backend_port=18080
web_port=13000
backend_origin="http://127.0.0.1:${backend_port}"
web_origin="http://localhost:${web_port}"
browser_origin="http://local.retrom.test:${web_port}"
process_id=""
previous_process_id=""
dev_log="$temporary_root/dev.log"

cleanup() {
  if [[ -r "$repository_root/.cache/retrom/dev.pid" ]]; then
    registered_pid=""
    read -r registered_pid _registered_start_ticks <"$repository_root/.cache/retrom/dev.pid" || true
    if [[ "$registered_pid" =~ ^[1-9][0-9]*$ ]] && \
      tr '\0' ' ' <"/proc/${registered_pid}/cmdline" 2>/dev/null | grep -q 'scripts/dev.sh'; then
      kill -TERM "$registered_pid" 2>/dev/null || true
    fi
  fi
  if [[ -n "$previous_process_id" ]]; then
    kill -TERM -- "-$previous_process_id" 2>/dev/null || true
    wait "$previous_process_id" 2>/dev/null || true
  fi
  if [[ -n "$process_id" ]]; then
    kill -TERM -- "-$process_id" 2>/dev/null || true
    wait "$process_id" 2>/dev/null || true
  fi
  rm -rf -- "$temporary_root"
}
trap cleanup EXIT

for port in "$backend_port" "$web_port"; do
  if ss -ltn "sport = :${port}" | tail -n +2 | grep -q .; then
    echo "acceptance port is already in use: ${port}" >&2
    exit 1
  fi
done

mkdir -p "$temporary_root/bin" "$temporary_root/data"
cat >"$temporary_root/bin/docker" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >>'$temporary_root/docker-calls.log'
exit 99
EOF
chmod +x "$temporary_root/bin/docker"
: >"$temporary_root/docker-calls.log"

start_dev() {
  setsid env \
    PATH="$temporary_root/bin:$PATH" \
    DOCKER="$temporary_root/bin/docker" \
    make dev \
      RETROM_DATA_DIR="$temporary_root/data" \
      RETROM_HTTP_ADDR="127.0.0.1:${backend_port}" \
      RETROM_PUBLIC_ORIGIN="$browser_origin" \
      NEXT_DEV_PORT="$web_port" \
      NEXT_BACKEND_ORIGIN="$backend_origin" \
      >"$dev_log" 2>&1 &
  process_id=$!
}

cd "$repository_root"
start_dev

wait_http() {
  local url="$1"
  local deadline=$((SECONDS + 90))
  until curl --fail --silent --show-error "$url" >"$temporary_root/response.tmp" 2>/dev/null; do
    if ! kill -0 "$process_id" 2>/dev/null; then
      sed 's/^/[dev] /' "$dev_log" >&2
      echo "make dev exited before ${url} became available" >&2
      exit 1
    fi
    if (( SECONDS >= deadline )); then
      sed 's/^/[dev] /' "$dev_log" >&2
      echo "timed out waiting for ${url}" >&2
      exit 1
    fi
    sleep 0.2
  done
}

wait_http "$backend_origin/health/live"
live="$(curl --fail --silent --show-error "$backend_origin/health/live")"
wait_http "$backend_origin/health/ready"
ready="$(curl --fail --silent --show-error "$backend_origin/health/ready")"
wait_http "$web_origin"

previous_process_id="$process_id"
dev_log="$temporary_root/dev-replacement.log"
start_dev
deadline=$((SECONDS + 20))
while kill -0 "$previous_process_id" 2>/dev/null; do
  if [[ "$(ps -o stat= -p "$previous_process_id" 2>/dev/null)" == Z* ]]; then
    break
  fi
  if ! kill -0 "$process_id" 2>/dev/null; then
    sed 's/^/[replacement] /' "$dev_log" >&2
    echo "replacement make dev exited before taking over" >&2
    exit 1
  fi
  if (( SECONDS >= deadline )); then
    sed 's/^/[replacement] /' "$dev_log" >&2
    echo "previous make dev process was not stopped" >&2
    exit 1
  fi
  sleep 0.1
done
set +e
wait "$previous_process_id"
previous_exit_status=$?
set -e
previous_process_id=""
wait_http "$backend_origin/health/ready"
wait_http "$web_origin"
grep -q 'stopping previous Retrom dev instance' "$dev_log"

home="$(curl --fail --silent --show-error "$web_origin/api/v1/home")"
hmr_status="$(python3 - "$web_port" "$browser_origin" <<'PY'
import base64
import os
import socket
import sys

port = int(sys.argv[1])
origin = sys.argv[2]
key = base64.b64encode(os.urandom(16)).decode("ascii")
request = (
    "GET /_next/hmr?id=acceptance HTTP/1.1\r\n"
    f"Host: localhost:{port}\r\n"
    f"Origin: {origin}\r\n"
    "Connection: Upgrade\r\n"
    "Upgrade: websocket\r\n"
    f"Sec-WebSocket-Key: {key}\r\n"
    "Sec-WebSocket-Version: 13\r\n\r\n"
)
with socket.create_connection(("127.0.0.1", port), timeout=5) as connection:
    connection.sendall(request.encode("ascii"))
    response = connection.recv(4096).decode("latin-1")
status = response.split("\r\n", 1)[0]
if status != "HTTP/1.1 101 Switching Protocols":
    raise SystemExit(f"HMR websocket upgrade failed: {status}")
print(status)
PY
)"

process_tree="$(python3 - "$process_id" <<'PY'
import subprocess
import sys

root = int(sys.argv[1])
rows = []
for line in subprocess.check_output(
    ["ps", "-eo", "pid=,ppid=,pgid=,args="], text=True
).splitlines():
    fields = line.strip().split(None, 3)
    if len(fields) < 4:
        continue
    rows.append((int(fields[0]), int(fields[1]), int(fields[2]), fields[3]))

descendants = {root}
changed = True
while changed:
    changed = False
    for pid, ppid, _pgid, _command in rows:
        if ppid in descendants and pid not in descendants:
            descendants.add(pid)
            changed = True

for pid, ppid, pgid, command in rows:
    if pid in descendants:
        print(f"{pid:8d} {ppid:8d} {pgid:8d} {command}")
PY
)"
listeners="$(ss -ltnp "sport = :${backend_port} or sport = :${web_port}")"
printf '%s\n' "$listeners" | grep -q "127.0.0.1:${backend_port}"
printf '%s\n' "$listeners" | grep -q "0.0.0.0:${web_port}"
if printf '%s\n' "$listeners" | grep -Eq "(0\.0\.0\.0|\[::\]):${backend_port}"; then
  echo "development backend listener escaped loopback" >&2
  exit 1
fi
if [[ -s "$temporary_root/docker-calls.log" ]]; then
  echo "make dev invoked Docker" >&2
  cat "$temporary_root/docker-calls.log" >&2
  exit 1
fi
if ! grep -q "go run ./cmd/retrom" <<<"$process_tree" || ! grep -q "next dev" <<<"$process_tree"; then
  echo "expected host Go and Next.js child processes were not found" >&2
  printf '%s\n' "$process_tree" >&2
  exit 1
fi

printf 'live=%s\nready=%s\nfront_end_home=%s\nhmr_status=%s\n' "$live" "$ready" "$home" "$hmr_status"
printf 'process_tree:\n%s\nlisteners:\n%s\n' "$process_tree" "$listeners"

read -r supervisor_pid _supervisor_start_ticks <"$repository_root/.cache/retrom/dev.pid"
kill -TERM "$supervisor_pid"
set +e
wait "$process_id"
exit_status=$?
set -e
process_id=""
if [[ $exit_status -ne 0 && $exit_status -ne 2 && $exit_status -ne 143 ]]; then
  echo "make dev exited with unexpected status ${exit_status}" >&2
  sed 's/^/[dev] /' "$dev_log" >&2
  exit 1
fi
if ! grep -q 'shutdown requested.*terminated' "$dev_log"; then
  echo "backend did not record an orderly SIGTERM shutdown" >&2
  sed 's/^/[dev] /' "$dev_log" >&2
  exit 1
fi

deadline=$((SECONDS + 5))
while ss -ltn "sport = :${backend_port} or sport = :${web_port}" | tail -n +2 | grep -q .; do
  if (( SECONDS >= deadline )); then
    echo "development child listener remained after SIGINT" >&2
    ss -ltnp "sport = :${backend_port} or sport = :${web_port}" >&2
    exit 1
  fi
  sleep 0.1
done

foreign_start_ticks="$(awk '{print $22}' "/proc/$$/stat")"
printf '%s %s\n' "$$" "$foreign_start_ticks" >"$repository_root/.cache/retrom/dev.pid"
"$repository_root/scripts/dev.sh" --stop
kill -0 "$$"
if [[ -e "$repository_root/.cache/retrom/dev.pid" ]]; then
  echo "stale or foreign dev PID registration was not cleaned" >&2
  exit 1
fi

printf 'docker_calls=0\nprevious_exit_status=%d\nexit_status=%d\nchildren_remaining=0\nforeign_process_preserved=1\n' \
  "$previous_exit_status" "$exit_status"
