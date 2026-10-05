#!/usr/bin/env bash
set -euo pipefail

image="${1:?backend image is required}"
container="retrom-image-check-$$"
database="${container}-postgres"
network="${container}-network"
database_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
cleanup() {
  docker rm -fv "$container" "$database" >/dev/null 2>&1 || true
  docker network rm "$network" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker network create --internal "$network" >/dev/null
docker run -d --name "$database" --network "$network" --network-alias postgres \
  --user 1000:1000 --tmpfs /var/lib/postgresql:rw,uid=1000,gid=1000,mode=0700 \
  -e PGDATA=/var/lib/postgresql/data \
  -e POSTGRES_USER=retrom -e POSTGRES_DB=retrom -e "POSTGRES_PASSWORD=$database_password" \
  postgres:18.3-bookworm@sha256:80630f83606d8db77d30b3851b16a9f78be2d0d4dda6f7b82a1fdca5ebe3acba >/dev/null
database_ready=false
for ((attempt = 0; attempt < 60; attempt++)); do
  if docker exec "$database" pg_isready -h 127.0.0.1 -U retrom -d retrom >/dev/null 2>&1; then
    database_ready=true
    break
  fi
  if [[ "$(docker inspect --format '{{.State.Running}}' "$database")" != true ]]; then
    break
  fi
  sleep 1
done
if [[ "$database_ready" != true ]]; then
  docker logs "$database"
  echo "Image verification PostgreSQL did not become ready" >&2
  exit 1
fi

docker create --name "$container" --network "$network" --user 1000:1000 \
  --tmpfs /var/lib/retrom:rw,uid=1000,gid=1000,mode=0700 \
  -e RETROM_PUBLIC_ORIGIN=https://retrom.example.com \
  -e "RETROM_DATABASE_URL=postgres://retrom:$database_password@postgres:5432/retrom?sslmode=disable" \
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
