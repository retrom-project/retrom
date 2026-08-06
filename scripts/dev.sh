#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
state_directory="$repository_root/.cache/retrom"
pid_file="$state_directory/dev.pid"
takeover_lock="$state_directory/dev-takeover.lock"
backend_pid=""
web_pid=""
process_start_ticks=""
mode="${1:-start}"

if [[ "$mode" != "start" && "$mode" != "--stop" ]]; then
  echo "usage: scripts/dev.sh [--stop]" >&2
  exit 2
fi

read_start_ticks() {
  local pid="$1"
  [[ -r "/proc/${pid}/stat" ]] || return 1
  awk '{print $22}' "/proc/${pid}/stat"
}

is_retrom_dev_process() {
  local pid="$1"
  local expected_start_ticks="$2"
  local actual_start_ticks=""
  local process_cwd=""
  local process_command=""

  [[ "$pid" =~ ^[1-9][0-9]*$ ]] || return 1
  actual_start_ticks="$(read_start_ticks "$pid")" || return 1
  [[ "$actual_start_ticks" == "$expected_start_ticks" ]] || return 1
  process_cwd="$(readlink -f "/proc/${pid}/cwd" 2>/dev/null)" || return 1
  [[ "$process_cwd" == "$repository_root" ]] || return 1
  process_command="$(tr '\0' ' ' <"/proc/${pid}/cmdline" 2>/dev/null)" || return 1
  [[ "$process_command" == *"scripts/dev.sh"* ]]
}

remove_own_pid_file() {
  local registered_pid=""
  local registered_start_ticks=""
  [[ -r "$pid_file" ]] || return 0
  read -r registered_pid registered_start_ticks <"$pid_file" || return 0
  if [[ "$registered_pid" == "$$" && "$registered_start_ticks" == "$process_start_ticks" ]]; then
    rm -f -- "$pid_file"
  fi
}

stop_children() {
  [[ -z "$backend_pid" ]] || kill -TERM -- "-$backend_pid" 2>/dev/null || true
  [[ -z "$web_pid" ]] || kill -TERM -- "-$web_pid" 2>/dev/null || true
  [[ -z "$backend_pid" ]] || wait "$backend_pid" 2>/dev/null || true
  [[ -z "$web_pid" ]] || wait "$web_pid" 2>/dev/null || true
}

cleanup() {
  local status=$?
  trap - INT TERM EXIT
  stop_children
  remove_own_pid_file
  exit "$status"
}

trap 'exit 130' INT
trap 'exit 143' TERM
trap cleanup EXIT

mkdir -p "$state_directory"
exec 9>"$takeover_lock"
flock -x 9

if [[ -r "$pid_file" ]]; then
  previous_pid=""
  previous_start_ticks=""
  read -r previous_pid previous_start_ticks <"$pid_file" || true
  if [[ "$previous_pid" != "$$" ]] && is_retrom_dev_process "$previous_pid" "$previous_start_ticks"; then
    printf 'stopping previous Retrom dev instance (pid %s)\n' "$previous_pid"
    kill -TERM "$previous_pid"
    deadline=$((SECONDS + 15))
    while is_retrom_dev_process "$previous_pid" "$previous_start_ticks"; do
      if (( SECONDS >= deadline )); then
        printf 'previous Retrom dev instance did not stop within 15 seconds (pid %s)\n' "$previous_pid" >&2
        exit 1
      fi
      sleep 0.1
    done
  else
    rm -f -- "$pid_file"
  fi
fi

if [[ "$mode" == "--stop" ]]; then
  exit 0
fi

process_start_ticks="$(read_start_ticks "$$")"
pid_file_tmp="${pid_file}.$$"
printf '%s %s\n' "$$" "$process_start_ticks" >"$pid_file_tmp"
mv -f -- "$pid_file_tmp" "$pid_file"
flock -u 9
exec 9>&-

setsid go run ./cmd/retrom &
backend_pid=$!
setsid bash -c 'cd "$1" && exec npm exec -- next dev --hostname "$2" --port "$3"' \
  retrom-dev-web "$repository_root/web" "${NEXT_DEV_HOST:-0.0.0.0}" "${NEXT_DEV_PORT:-3000}" &
web_pid=$!

while kill -0 "$backend_pid" 2>/dev/null && kill -0 "$web_pid" 2>/dev/null; do
  status=0
  wait -n "$backend_pid" "$web_pid" || status=$?
  if ! kill -0 "$backend_pid" 2>/dev/null || ! kill -0 "$web_pid" 2>/dev/null; then
    exit "$status"
  fi
done
