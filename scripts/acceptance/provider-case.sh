#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNTIME_ROOT="${RETROM_RUNTIME_ROOT:-$(cd "$ROOT/../retrom-runtime" && pwd)}"
NPM="$ROOT/.cache/tools/node-v24.18.0-linux-x64/bin/npm"
GO="$ROOT/.cache/tools/go1.26.5-linux-amd64/bin/go"
CASE_ID="${1:-}"

if [[ ! -x "$NPM" || ! -x "$GO" || ! -f "$RUNTIME_ROOT/package.json" ]]; then
  echo "PROVIDER_ACCEPTANCE_TOOLCHAIN_MISSING" >&2
  exit 2
fi

runtime_test() {
  "$NPM" --prefix "$RUNTIME_ROOT" test -- --run "$@"
}

web_test() {
  "$NPM" --prefix "$ROOT/web" test -- --run "$@"
}

python_test() {
  (cd "$ROOT" && PYTHONPATH="$ROOT/scripts${PYTHONPATH:+:$PYTHONPATH}" python3 -m unittest "$@")
}

case "$CASE_ID" in
  ACC-PROVIDER-001)
    python_test scripts/test_runtime_provider_contract.py scripts/test_runtime_providers.py
    runtime_test tests/provider-bundle.test.ts tests/provider-release-build.test.ts \
      tests/emulatorjs-provider-release-build.test.ts tests/emulatorjs-provider-input.test.ts
    ;;
  ACC-PROVIDER-002)
    python_test scripts/test_runtime_target_bindings.py
    (cd "$ROOT" && "$GO" test ./internal/runtimecatalog -count=1)
    runtime_test src/provider/declarations.test.ts src/providers/emulatorjs/catalog.test.ts \
      src/providers/retrom-runtime/module-config.test.ts tests/repository-boundary.test.ts
    ;;
  ACC-PROVIDER-003)
    (cd "$ROOT" && "$GO" test ./internal/runtimeoptions -count=1)
    (cd "$ROOT" && "$GO" test ./internal/runtimebundle ./internal/runtimelaunch ./internal/httpapi \
      -run 'Provider|LaunchEnvelope|RuntimeStatic|RuntimeAsset' -count=1)
    web_test features/player/runtime/envelope-fixtures.test.ts \
      features/player/runtime/provider-module-v1.test.ts features/player/runtime/runtime-controller.test.ts \
      features/player/runtime/runtime-host.test.ts
    ;;
  ACC-PROVIDER-004)
    (cd "$ROOT" && "$GO" test ./internal/saves ./internal/rpgmaker/runtimevalidation -count=1)
    runtime_test src/providers/emulatorjs/state-restore.test.ts src/providers/retrom-runtime/module.test.ts
    web_test features/player/runtime/runtime-actions.test.ts features/player/runtime/runtime-host.test.ts \
      features/player/player-session.test.tsx features/player/rpg-runtime-validation.test.ts
    ;;
  ACC-PROVIDER-005)
    (cd "$ROOT" && "$GO" test ./internal/runtimeprovider -count=1)
    (cd "$ROOT" && "$GO" test -tags=integration ./internal/rpgmaker/packs -count=1)
    (cd "$ROOT" && "$GO" test -tags=integration ./internal/saves -run 'TestCatalogExtensionPreservesInitializedGamesReviewsSettingsAndSaves' -count=1)
    (cd "$ROOT" && "$GO" test ./internal/store -run 'TestTerminalReviewReleasesTemporaryCheckpointButKeepsBlobEvidence' -count=1)
    python_test scripts/test_runtime_providers.py
    ;;
  ACC-PROVIDER-006)
    python_test scripts/test_pfb.py scripts/test_release_input_digest.py scripts/test_makefile.py
    runtime_test tests/provider-build-metadata.test.ts tests/provider-source-boundary.test.ts \
      tests/provider-release-build.test.ts
    ;;
  ACC-PROVIDER-007)
    runtime_test src/providers/emulatorjs
    web_test features/player/player-session.test.tsx features/player/multi-disc-telemetry.test.ts \
      features/player/immersive-controls.test.ts features/player/immersive-gamepad-filter.test.ts \
      features/player/use-immersive-player.test.tsx features/player/netplay \
      features/player/runtime/netplay-port-adapter.test.ts
    ;;
  ACC-PROVIDER-008)
    runtime_test src/providers/retrom-runtime tests/repository-boundary.test.ts
    (cd "$ROOT" && "$GO" test ./internal/rpgmaker/runtimevalidation ./internal/httpapi \
      -run 'RPG|Review|Provider|UniqueOrigin|Isolation' -count=1)
    web_test features/reviews/review-preview-provider-authority.test.ts \
      features/reviews/review-rpg-actions.test.tsx features/reviews/review-rpg-validation.test.tsx \
      features/player/rpg-runtime-validation.test.ts
    ;;
  *)
    echo "usage: provider-case.sh ACC-PROVIDER-001..008" >&2
    exit 2
    ;;
esac
