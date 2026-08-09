#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
state_directory="$repository_root/.cache/retrom"
pid_file="$state_directory/dev.pid"
takeover_lock="$state_directory/dev-takeover.lock"
data_root="${RETROM_DATA_DIR:-$state_directory/data}"
data_root_lock="$data_root/retrom.lock"
auth_mode="${RETROM_MODE:-test}"
backend_pid=""
web_pid=""
process_start_ticks=""
registration_version=""
registered_supervisor_pid=""
registered_supervisor_start_ticks=""
registered_backend_pid=""
registered_backend_start_ticks=""
registered_web_pid=""
registered_web_start_ticks=""
mode="${1:-start}"

if [[ "$mode" != "start" && "$mode" != "--stop" ]]; then
  echo "usage: scripts/dev.sh [--stop]" >&2
  exit 2
fi
if [[ "$auth_mode" != "release" && "$auth_mode" != "test" ]]; then
  echo "RETROM_MODE must be release or test" >&2
  exit 2
fi

read_process_identity() {
  local pid="$1"
  local stat_line=""
  local fields=""
  [[ -r "/proc/${pid}/stat" ]] || return 1
  IFS= read -r stat_line <"/proc/${pid}/stat" || return 1
  fields="${stat_line##*) }"
  [[ "$fields" != "$stat_line" ]] || return 1
  set -- $fields
  [[ $# -ge 20 ]] || return 1
  printf '%s %s %s %s\n' "${20}" "$2" "$3" "$4"
}

read_start_ticks() {
  local identity=""
  identity="$(read_process_identity "$1")" || return 1
  printf '%s\n' "${identity%% *}"
}

is_positive_integer() {
  [[ "$1" =~ ^[1-9][0-9]*$ ]]
}

load_registration() {
  local first=""
  local second=""
  local third=""
  local fourth=""
  local fifth=""
  local sixth=""
  local seventh=""
  local extra=""

  registration_version=""
  registered_supervisor_pid=""
  registered_supervisor_start_ticks=""
  registered_backend_pid=""
  registered_backend_start_ticks=""
  registered_web_pid=""
  registered_web_start_ticks=""
  [[ -r "$pid_file" ]] || return 1
  read -r first second third fourth fifth sixth seventh extra <"$pid_file" || return 1
  if [[ "$first" == "v2" ]]; then
    [[ -z "$extra" ]] || return 1
    is_positive_integer "$second" && is_positive_integer "$third" && \
      is_positive_integer "$fourth" && is_positive_integer "$fifth" && \
      is_positive_integer "$sixth" && is_positive_integer "$seventh" || return 1
    registration_version="v2"
    registered_supervisor_pid="$second"
    registered_supervisor_start_ticks="$third"
    registered_backend_pid="$fourth"
    registered_backend_start_ticks="$fifth"
    registered_web_pid="$sixth"
    registered_web_start_ticks="$seventh"
    return 0
  fi
  [[ -z "$third" ]] || return 1
  is_positive_integer "$first" && is_positive_integer "$second" || return 1
  registration_version="v1"
  registered_supervisor_pid="$first"
  registered_supervisor_start_ticks="$second"
}

is_retrom_dev_process() {
  local pid="$1"
  local expected_start_ticks="$2"
  local identity=""
  local actual_start_ticks=""
  local process_cwd=""
  local process_command=""

  is_positive_integer "$pid" || return 1
  identity="$(read_process_identity "$pid")" || return 1
  actual_start_ticks="${identity%% *}"
  [[ "$actual_start_ticks" == "$expected_start_ticks" ]] || return 1
  process_cwd="$(readlink -f "/proc/${pid}/cwd" 2>/dev/null)" || return 1
  [[ "$process_cwd" == "$repository_root" ]] || return 1
  process_command="$(tr '\0' ' ' <"/proc/${pid}/cmdline" 2>/dev/null)" || return 1
  [[ "$process_command" == *"scripts/dev.sh"* ]]
}

is_registered_child() {
  local pid="$1"
  local expected_start_ticks="$2"
  local expected_cwd="$3"
  local command_marker="$4"
  local identity=""
  local actual_start_ticks=""
  local _parent_pid=""
  local process_group_id=""
  local session_id=""
  local process_cwd=""
  local process_command=""

  is_positive_integer "$pid" || return 1
  identity="$(read_process_identity "$pid")" || return 1
  read -r actual_start_ticks _parent_pid process_group_id session_id <<<"$identity"
  [[ "$actual_start_ticks" == "$expected_start_ticks" ]] || return 1
  [[ "$process_group_id" == "$pid" && "$session_id" == "$pid" ]] || return 1
  process_cwd="$(readlink -f "/proc/${pid}/cwd" 2>/dev/null)" || return 1
  [[ "$process_cwd" == "$expected_cwd" ]] || return 1
  process_command="$(tr '\0' ' ' <"/proc/${pid}/cmdline" 2>/dev/null)" || return 1
  [[ "$process_command" == *"$command_marker"* ]]
}

is_registered_backend() {
  is_registered_child \
    "$registered_backend_pid" \
    "$registered_backend_start_ticks" \
    "$repository_root" \
    "go run ./cmd/retrom"
}

is_registered_web() {
  is_registered_child \
    "$registered_web_pid" \
    "$registered_web_start_ticks" \
    "$repository_root/web" \
    "next dev --hostname ${NEXT_DEV_HOST:-0.0.0.0} --port ${NEXT_DEV_PORT:-3000}"
}

data_root_lock_available() {
  [[ -e "$data_root_lock" ]] || return 0
  exec 8<>"$data_root_lock" || return 1
  if flock -n 8; then
    flock -u 8
    exec 8>&-
    return 0
  fi
  exec 8>&-
  return 1
}

remove_own_pid_file() {
  load_registration || return 0
  if [[ "$registered_supervisor_pid" == "$$" && \
    "$registered_supervisor_start_ticks" == "$process_start_ticks" ]]; then
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

if load_registration; then
  if [[ "$registered_supervisor_pid" != "$$" ]] && \
    is_retrom_dev_process "$registered_supervisor_pid" "$registered_supervisor_start_ticks"; then
    printf 'stopping previous Retrom dev instance (pid %s)\n' "$registered_supervisor_pid"
    kill -TERM "$registered_supervisor_pid"
    deadline=$((SECONDS + 15))
    while is_retrom_dev_process "$registered_supervisor_pid" "$registered_supervisor_start_ticks"; do
      if (( SECONDS >= deadline )); then
        printf 'previous Retrom dev instance did not stop within 15 seconds (pid %s)\n' \
          "$registered_supervisor_pid" >&2
        exit 1
      fi
      sleep 0.1
    done
    deadline=$((SECONDS + 15))
    while ! data_root_lock_available; do
      if (( SECONDS >= deadline )); then
        printf 'previous Retrom backend did not release its data lock within 15 seconds (%s)\n' "$data_root_lock" >&2
        exit 1
      fi
      sleep 0.1
    done
  elif [[ "$registration_version" == "v2" ]]; then
    registered_backend_active=false
    registered_web_active=false
    if is_registered_backend; then
      registered_backend_active=true
    fi
    if is_registered_web; then
      registered_web_active=true
    fi
    if [[ "$registered_backend_active" == true || "$registered_web_active" == true ]]; then
      printf 'recovering orphaned Retrom dev children (backend pid %s, web pid %s)\n' \
        "$registered_backend_pid" "$registered_web_pid"
      if [[ "$registered_backend_active" == true ]]; then
        kill -TERM -- "-$registered_backend_pid"
      fi
      if [[ "$registered_web_active" == true ]]; then
        kill -TERM -- "-$registered_web_pid"
      fi
      deadline=$((SECONDS + 15))
      while { is_registered_backend || is_registered_web; }; do
        if (( SECONDS >= deadline )); then
          echo 'orphaned Retrom dev children did not stop within 15 seconds' >&2
          exit 1
        fi
        sleep 0.1
      done
      deadline=$((SECONDS + 15))
      while ! data_root_lock_available; do
        if (( SECONDS >= deadline )); then
          echo 'orphaned Retrom backend did not release its data lock within 15 seconds' >&2
          exit 1
        fi
        sleep 0.1
      done
    fi
  fi
  rm -f -- "$pid_file"
elif [[ -e "$pid_file" ]]; then
  rm -f -- "$pid_file"
fi

if ! data_root_lock_available; then
  echo 'Retrom data root is locked by a process that this repository cannot safely identify' >&2
  exit 1
fi

if [[ "$mode" == "--stop" ]]; then
  exit 0
fi

process_start_ticks="$(read_start_ticks "$$")"
setsid env -u RETROM_MODE go run ./cmd/retrom --mode="$auth_mode" &
backend_pid=$!
setsid bash -c 'cd "$1" && exec npm exec -- next dev --hostname "$2" --port "$3"' \
  retrom-dev-web "$repository_root/web" "${NEXT_DEV_HOST:-0.0.0.0}" "${NEXT_DEV_PORT:-3000}" &
web_pid=$!
backend_start_ticks="$(read_start_ticks "$backend_pid")"
web_start_ticks="$(read_start_ticks "$web_pid")"
pid_file_tmp="${pid_file}.$$"
printf 'v2 %s %s %s %s %s %s\n' \
  "$$" "$process_start_ticks" \
  "$backend_pid" "$backend_start_ticks" \
  "$web_pid" "$web_start_ticks" \
  >"$pid_file_tmp"
mv -f -- "$pid_file_tmp" "$pid_file"
flock -u 9
exec 9>&-

while kill -0 "$backend_pid" 2>/dev/null && kill -0 "$web_pid" 2>/dev/null; do
  status=0
  wait -n "$backend_pid" "$web_pid" || status=$?
  if ! kill -0 "$backend_pid" 2>/dev/null || ! kill -0 "$web_pid" 2>/dev/null; then
    exit "$status"
  fi
done
