#!/usr/bin/env bash
set -euo pipefail
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repository_root"
export PATH="$repository_root/.cache/tools/node-v24.18.0-linux-x64/bin:$PATH"
export RETROM_WEB_ORIGIN
RETROM_WEB_ORIGIN="$(python3 - <<'PY'
import json
from pathlib import Path
spec = json.loads(Path('.pfb/spec.json').read_text())
print('http://' + spec['id'] + '.localhost:3000')
PY
)"
RETROM_PFB_DIAGNOSTICS_REQUIRED=1 web/node_modules/.bin/playwright test -c web/playwright.config.ts \
  web/e2e/player-input-diagnostics.spec.ts --project=chrome-1280
