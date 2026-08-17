#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

cd "$repository_root"
make data-check
go test ./internal/dependencies ./internal/httpapi -run 'TestBootstrapMaterializedDependencies|TestPlatformImportCapabilitiesUseFeaturePlatformAndArtifactIntersection|TestCreateImportContentModeDefaultsToStandardAndMapsMultiDiscAdmissionErrors' -count=1
go test -tags=integration ./internal/gamecontent -run '^TestMultiDiscReplacementPublishesCompleteRevisionAndRejectsMissingDisc$' -count=1 -timeout=60s
.cache/tools/node-v24.18.0-linux-x64/bin/npm --prefix web test -- \
  --run features/player/adapters/ejs-4.2.3-v2.test.ts features/player/multi-disc-restore.test.ts
