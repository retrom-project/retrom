#!/usr/bin/env bash

remove_dev_dist() {
  local dist_directory="$1"
  local deadline=$((SECONDS + 5))
  while [[ -e "$dist_directory" ]]; do
    rm -rf -- "$dist_directory" 2>/dev/null || true
    [[ ! -e "$dist_directory" ]] && return 0
    if (( SECONDS >= deadline )); then
      echo "failed to remove acceptance build directory: $dist_directory" >&2
      return 1
    fi
    sleep 0.1
  done
}
