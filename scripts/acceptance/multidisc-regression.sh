#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

cd "$repository_root"
make data-check
go test ./internal/dependencies ./internal/httpapi -run 'TestBootstrapMaterializedDependencies|TestPlatformImportCapabilitiesUseFeaturePlatformAndArtifactIntersection|TestCreateImportContentModeDefaultsToStandardAndMapsMultiDiscAdmissionErrors' -count=1
go test -tags=integration ./internal/gamecontent -run '^TestMultiDiscReplacementPublishesCompleteContentAndRejectsMissingDisc$' -count=1 -timeout=60s
scripts/acceptance/provider-case.sh ACC-PROVIDER-007
