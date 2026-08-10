#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
run_dir=${RETROM_ACCEPTANCE_RUN_DIR:?RETROM_ACCEPTANCE_RUN_DIR is required}

cd "$repository_root"
make data-check
go test ./internal/dependencies ./internal/httpapi -run 'TestBootstrapMaterializedDependencies|TestPlatformImportCapabilitiesUseFeaturePlatformAndArtifactIntersection|TestCreateImportContentModeDefaultsToStandardAndMapsMultiDiscAdmissionErrors' -count=1
go test -tags=integration ./internal/gamecontent -run '^TestMultiDiscReplacementPublishesCompleteRevisionAndRejectsMissingDisc$' -count=1 -timeout=60s

python3 - "$run_dir" <<'PY'
import json
import pathlib
import sys

run_dir = pathlib.Path(sys.argv[1])
missing = []
invalid = []
for number in range(1, 29):
    case_id = f"ACC-CORE-{number:03d}"
    result_path = run_dir / "cases" / case_id.lower() / "result.json"
    if not result_path.is_file():
        missing.append(case_id)
        continue
    result = json.loads(result_path.read_text(encoding="utf-8"))
    if result.get("status") != "PASS":
        invalid.append(f"{case_id}:{result.get('status')}")
if missing or invalid:
    raise SystemExit(f"ACC-MDISC-007 requires current-run core PASS evidence; missing={missing}, invalid={invalid}")
print("All ACC-CORE-001..028 results are PASS in the current acceptance run.")
PY
