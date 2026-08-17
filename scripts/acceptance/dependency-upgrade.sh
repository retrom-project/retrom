#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

cd "$repository_root"
make data-check
make deps-check
go test -tags=integration ./internal/dependencies \
  -run 'TestBootstrapMaterializedDependencies|TestBootstrapCatalogsMaterializesPinnedDATsIdempotently' \
  -count=1
go test ./internal/dependencies ./internal/launch \
  -run 'TestSelectedCoreStartupActionDelayBoundary|TestArtifactCompatibilityV2Validation' \
  -count=1
go test -tags='integration localfixtures' ./internal/libraryimport \
  -run '^TestFBA2012RealDATImportVariantAndLaunchIsolation$' \
  -count=1 -timeout=120s
make web-test
make web-e2e

echo "dependency upgrade audit: manifests, payloads, adapters, bootstrap, action bounds, and Retrom product E2E passed"
