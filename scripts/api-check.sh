#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$repository_root/.cache/tools/node-v24.18.0-linux-x64/bin:$PATH"

tmp_dir="$(mktemp -d)"
cleanup() { rm -rf -- "$tmp_dir"; }
trap cleanup EXIT

mkdir -p "$tmp_dir/internal/httpapi/generated" "$tmp_dir/web/lib/api/generated"
ln -s "$repository_root/go.mod" "$tmp_dir/go.mod"
ln -s "$repository_root/go.sum" "$tmp_dir/go.sum"
(
	cd "$tmp_dir"
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 \
	  --config "$repository_root/api/oapi-codegen.yaml" \
	  "$repository_root/api/openapi.yaml"
)
(
  cd "$repository_root/web"
  npx --no-install openapi-typescript "$repository_root/api/openapi.yaml" \
    -o "$tmp_dir/web/lib/api/generated/schema.d.ts"
)
cmp "$repository_root/internal/httpapi/generated/api.gen.go" "$tmp_dir/internal/httpapi/generated/api.gen.go"
cmp "$repository_root/web/lib/api/generated/schema.d.ts" "$tmp_dir/web/lib/api/generated/schema.d.ts"
