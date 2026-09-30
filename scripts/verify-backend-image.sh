#!/usr/bin/env bash
set -euo pipefail

image="${1:?backend image is required}"
container="retrom-image-check-$$"
trap 'docker rm -f "$container" >/dev/null 2>&1 || true' EXIT

docker create --name "$container" --user 1000:1000 \
  --tmpfs /var/lib/retrom:rw,uid=1000,gid=1000,mode=0700 \
  -e RETROM_PUBLIC_ORIGIN=https://retrom.example.com \
  -e 'RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE=https://{launchId}.sub.retrom.example.com' \
  "$image" >/dev/null
docker start "$container" >/dev/null
for ((attempt = 0; attempt < 60; attempt++)); do
  if docker exec "$container" wget -q -O /dev/null http://127.0.0.1:8080/health/ready 2>/dev/null; then
    echo "Backend image is ready as UID/GID 1000:1000"
    exit 0
  fi
  if [[ "$(docker inspect --format '{{.State.Running}}' "$container")" != true ]]; then
    docker logs "$container"
    exit 1
  fi
  sleep 1
done
docker logs "$container"
echo "Backend image did not become ready within 60 seconds" >&2
exit 1
