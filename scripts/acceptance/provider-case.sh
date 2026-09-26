#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNTIME_ROOT="${RETROM_RUNTIME_ROOT:-$(cd "$ROOT/../retrom-runtime" && pwd)}"
NPM="${NODE_HOME:-$ROOT/.cache/tools/node-v24.18.0-linux-x64}/bin/npm"
# make prepare-go can select the exact system toolchain without a local cache.
GO="$(command -v go || true)"
GO_VERSION="$(awk '$1 == "go" {print $2; exit}' "$ROOT/go.mod")"
CASE_ID="${1:-}"

if [[ ! -x "$NPM" || ! -x "$GO" || ! -f "$RUNTIME_ROOT/package.json" ]]; then
  echo "PROVIDER_ACCEPTANCE_TOOLCHAIN_MISSING" >&2
  exit 2
fi
if [[ "$("$GO" env GOVERSION)" != "go$GO_VERSION" ]]; then
  echo "PROVIDER_ACCEPTANCE_GO_VERSION_MISMATCH" >&2
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
      src/providers/retrom-runtime/target-adapter.test.ts tests/repository-boundary.test.ts
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
    (cd "$ROOT" && "$GO" test -tags=integration ./internal/service/saves ./internal/persistence/saves ./internal/launch -count=1)
    (cd "$ROOT" && "$GO" test -tags=integration ./internal/httpapi \
      -run 'OrdinaryReviewCheckpointHTTP' -count=1)
    runtime_test src/providers/emulatorjs/state-restore.test.ts src/providers/retrom-runtime/module.test.ts
    web_test features/player/runtime/runtime-actions.test.ts features/player/runtime/runtime-host.test.ts \
      features/player/player-session.test.tsx features/player/review-preview-receipt.test.ts \
      features/player/player-checkpoint-availability.test.ts
    ;;
  ACC-PROVIDER-005)
    (cd "$ROOT" && "$GO" test ./internal/runtimeprovider ./internal/service/runtimeprovider ./internal/persistence/runtimeprovider -count=1)
    (cd "$ROOT" && "$GO" test -tags=integration ./internal/service/saves ./internal/persistence/saves -run 'TestCatalogExtensionPreservesInitializedGamesReviewsSettingsAndSaves' -count=1)
    (cd "$ROOT" && "$GO" test -tags=integration ./internal/launch \
      -run 'TestReviewCheckpointIsScopedExpiringAndReleasedByOrdinaryGC|TestPublishingReviewReleasesAllTemporaryPreviewOwners' -count=1)
    python_test scripts/test_runtime_providers.py
    ;;
  ACC-PROVIDER-006)
    python_test scripts/test_pfb.py scripts/test_release_input_digest.py scripts/test_makefile.py \
      scripts/test_runtime_providers.py scripts/test_runtime_provider_release.py
    (cd "$ROOT" && "$GO" test ./internal/runtimebundle -run TestProductionProviderVersionsFollowReleaseTag -count=1)
    runtime_test tests/provider-build-metadata.test.ts tests/provider-source-boundary.test.ts \
      tests/provider-release-build.test.ts tests/release-version.test.ts \
      tests/provider-client-build.test.ts tests/pfb-provider-dev.test.ts
    ;;
  ACC-PROVIDER-007)
    runtime_test src/providers/emulatorjs
    web_test features/player/player-session.test.tsx features/player/multi-disc-telemetry.test.ts \
      features/player/immersive-controls.test.ts features/player/immersive-gamepad-filter.test.ts \
      features/player/use-immersive-player.test.tsx
    ;;
  ACC-PROVIDER-008)
    runtime_test src/providers/retrom-runtime tests/repository-boundary.test.ts
    (cd "$ROOT" && "$GO" test -tags=integration ./internal/launch ./internal/httpapi \
      -run 'RPG|Review|Provider|UniqueOrigin|Isolation' -count=1)
    web_test features/reviews/review-preview-provider-authority.test.ts \
      features/reviews/review-rpg-actions.test.tsx features/reviews/review-rpg-dependencies.test.tsx \
      features/player/review-preview-receipt.test.ts features/player/review-preview-screenshot.test.ts
    ;;
  *)
    echo "usage: provider-case.sh ACC-PROVIDER-001..008" >&2
    exit 2
    ;;
esac
