#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repository_root"

go test -tags=integration ./internal/launch ./internal/libraryimport ./internal/saves \
  -run 'TestPublishedGameLaunchLocksContentAndCredential|TestArcadeImportUsesInstalledBIOSBeforeCreatingReview|TestManualStateRequiresAtomicNonEmptyStateAndScreenshot' \
  -count=1 -timeout=120s

PATH="$repository_root/.cache/tools/node-v24.18.0-linux-x64/bin:$PATH"
export PATH
npm --prefix web test -- --run \
  features/player/player-shell.test.ts \
  features/player/player-chrome.test.tsx \
  features/player/launch-button.test.tsx \
  features/player/explicit-state-restore.test.ts \
  features/player/transient-save-storage.test.ts \
  features/player/pause-control.test.ts

make web-build

for forbidden in \
  '__RETROM_NETPLAY_DIAGNOSTICS_FACTORY__' \
  'NETPLAY_DESYNC_INJECTION_FAILED' \
  'netplayAcceptance'; do
  while IFS= read -r -d '' asset; do
    if grep -I -F -q -- "$forbidden" "$asset"; then
      printf 'production test hook %s found in %s\n' "$forbidden" "$asset" >&2
      exit 1
    fi
  done < <(find web/.next-build -type f \( -name '*.js' -o -name '*.html' \) -print0)
done

printf 'single_player_regression=passed\nproduction_test_hooks=absent\n'
