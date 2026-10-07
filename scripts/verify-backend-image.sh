#!/usr/bin/env bash
set -euo pipefail

image="${1:?backend image is required}"
web_image="${2:?web image is required}"
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d -t retrom-image-source.XXXXXXXX)"
container="retrom-image-check-$$"
database="${container}-postgres"
network="${container}-network"
redis="${container}-redis"
web="${container}-web"
database_password="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
cleanup() {
  docker rm -fv "$container" "$database" "$redis" "$web" >/dev/null 2>&1 || true
  docker network rm "$network" >/dev/null 2>&1 || true
  rm -rf "$fixture"
}
trap cleanup EXIT

python3 - "$repository_root" "$fixture" <<'PY'
import importlib.util
from pathlib import Path
import sys
import uuid
root, fixture = map(Path, sys.argv[1:])
spec = importlib.util.spec_from_file_location("image_rom", root / "testdata/public-roms/nes-smoke/build.py")
builder = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = builder
spec.loader.exec_module(builder)
source = fixture / "browser"
source.mkdir()
(source / "image.nes").write_bytes(builder.build_rom(("IMAGE MIT " + uuid.uuid4().hex[:8]).encode()))
(source / "metadata.pegasus.txt").write_text("collection: Image NES\nshortname: nes\nextensions: nes\n\ngame: Image NES\nfile: image.nes\n")
fixture.chmod(0o755)
source.chmod(0o755)
for file in source.iterdir(): file.chmod(0o444)
PY

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

docker run -d --name "$redis" --network "$network" --network-alias redis \
  redis:8.2.2-bookworm@sha256:4521b581dbddea6e7d81f8fe95ede93f5648aaa66a9dacd581611bf6fe7527bd redis-server --save '' --appendonly no >/dev/null
docker create --name "$container" --network "$network" --network-alias retrom --user 1000:1000 \
  --tmpfs /var/lib/retrom:rw,uid=1000,gid=1000,mode=0700 \
  --mount "type=bind,source=$fixture,target=/image-source,readonly" \
  -e RETROM_PUBLIC_ORIGIN=https://retrom.example.com \
  -e RETROM_REDIS_ADDR=redis:6379 \
  -e 'RETROM_SOURCE_ROOTS=[{"id":"image","name":"Image verification","path":"/image-source"}]' \
  -e "RETROM_DATABASE_URL=postgres://retrom:$database_password@postgres:5432/retrom?sslmode=disable" \
  -e 'RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE=https://{runId}.sub.retrom.example.com' \
  "$image" >/dev/null
docker start "$container" >/dev/null
for ((attempt = 0; attempt < 60; attempt++)); do
  if docker exec "$container" node -e 'fetch("http://127.0.0.1:8080/health/ready").then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))' 2>/dev/null; then
    echo "Backend image is ready as UID/GID 1000:1000"
    docker exec -i "$container" node --input-type=module - < "$repository_root/scripts/verify-backend-image.mjs"
    docker run -d --name "$web" --network "$network" --user 1000:1000 "$web_image" >/dev/null
    for ((web_attempt = 0; web_attempt < 60; web_attempt++)); do
      if docker exec "$web" node -e 'fetch("http://127.0.0.1:3000/health/ready").then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))' 2>/dev/null; then
        docker exec -i "$web" node --input-type=module - < "$repository_root/scripts/verify-web-image.mjs"
        exit 0
      fi
      if [[ "$(docker inspect --format '{{.State.Running}}' "$web")" != true ]]; then
        docker logs "$web"
        exit 1
      fi
      sleep 1
    done
    docker logs "$web"
    echo "Web image did not reach the backend through its built rewrite within 60 seconds" >&2
    exit 1
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
