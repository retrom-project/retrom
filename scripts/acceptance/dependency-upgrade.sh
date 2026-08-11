#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
run_directory="${RETROM_ACCEPTANCE_RUN_DIR:-}"

if [[ -z "$run_directory" || ! -f "$run_directory/run.json" ]]; then
  echo "RETROM_ACCEPTANCE_RUN_DIR must name a prepared acceptance run" >&2
  exit 1
fi

cd "$repository_root"
make data-check
make deps-check
go test -tags=integration ./internal/dependencies \
  -run 'TestBootstrapMaterializedDependencies|TestBootstrapCatalogsMaterializesPinnedDATsIdempotently' \
  -count=1
go test ./internal/dependencies ./internal/launch \
  -run 'TestSelectedCoreStartupActionDelayBoundary|TestArtifactCompatibilityV2Validation' \
  -count=1
make web-test

for number in $(seq 1 35); do
  case_id="$(printf 'acc-core-%03d' "$number")"
  result="$run_directory/cases/$case_id/result.json"
  if [[ ! -f "$result" ]] || [[ "$(jq -r '.status' "$result")" != "PASS" ]]; then
    echo "current acceptance run is missing PASS evidence for ${case_id^^}" >&2
    exit 1
  fi
done

echo "dependency upgrade audit: manifests, payloads, adapters, bootstrap, action bounds, and 35 independent core cases passed"
