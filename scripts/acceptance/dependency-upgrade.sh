#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

cd "$repository_root"
make data-check
make deps-check
go test -tags=integration ./internal/adapter/runtime/dependencies ./internal/service/dependencies ./internal/persistence/dependencies \
  -run 'TestBootstrapMaterializedDependencies|TestBootstrapCatalogsMaterializesPinnedDATsIdempotently' \
  -count=1
go test ./internal/adapter/runtime/dependencies ./internal/service/dependencies ./internal/persistence/dependencies ./internal/adapter/runtime/launch \
  -run 'TestSelectedCoreStartupActionDelayBoundary|TestArtifactCompatibilityV2Validation' \
  -count=1
make web-test
make web-e2e

echo "dependency upgrade audit: manifests, payloads, adapters, bootstrap, action bounds, and Retrom product E2E passed"
