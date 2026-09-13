#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

cd "$repository_root"
make data-check
go test ./internal/adapter/runtime/dependencies ./internal/service/dependencies ./internal/persistence/dependencies ./internal/transport/httpapi -run 'TestBootstrapMaterializedDependencies|TestPlatformImportCapabilitiesUseFeaturePlatformAndArtifactIntersection|TestCreateImportContentModeDefaultsToStandardAndMapsMultiDiscAdmissionErrors' -count=1
go test -tags=integration ./internal/service/gamecontent ./internal/persistence/gamecontent -run '^TestMultiDiscReplacementPublishesCompleteContentAndRejectsMissingDisc$' -count=1 -timeout=60s
scripts/acceptance/provider-case.sh ACC-PROVIDER-007
