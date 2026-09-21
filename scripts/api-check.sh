#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$repository_root/.cache/tools/node-v24.18.0-linux-x64/bin:$PATH"

tmp_dir="$(mktemp -d)"
cleanup() { rm -rf -- "$tmp_dir"; }
trap cleanup EXIT

mkdir -p "$tmp_dir/internal/httpapi/generated" "$tmp_dir/web/lib/api/generated" "$tmp_dir/generated"
ln -s "$repository_root/go.mod" "$tmp_dir/go.mod"
ln -s "$repository_root/go.sum" "$tmp_dir/go.sum"
(
	cd "$repository_root"
	go run ./scripts/openapi-bundle \
	  -input api/openapi.yaml \
	  -output "$tmp_dir/generated/openapi.bundle.yaml"
)
(
	cd "$tmp_dir"
	for config in "$repository_root"/api/codegen/*.yaml; do
		go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 \
		  --config "$config" \
		  "$tmp_dir/generated/openapi.bundle.yaml"
	done
)
(
  cd "$repository_root/web"
  npx --no-install openapi-typescript "$tmp_dir/generated/openapi.bundle.yaml" \
    -o "$tmp_dir/web/lib/api/generated/schema.d.ts"
)
for generated in models.gen.go server.gen.go spec.gen.go; do
  path="internal/httpapi/generated/$generated"
  if git -C "$repository_root" ls-files --error-unmatch "$path" >/dev/null 2>&1; then
    echo "$path is generated during backend builds and must not be tracked" >&2
    exit 1
  fi
  git -C "$repository_root" check-ignore --quiet "$path"
done
cmp "$repository_root/web/lib/api/generated/schema.d.ts" "$tmp_dir/web/lib/api/generated/schema.d.ts"
