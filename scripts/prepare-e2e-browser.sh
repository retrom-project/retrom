#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
node="$repository_root/.cache/tools/node-v24.18.0-linux-x64/bin/node"
playwright_cli="$repository_root/web/node_modules/playwright/cli.js"
playwright_manifest="$repository_root/web/node_modules/playwright-core/browsers.json"
tools_directory="$repository_root/.cache/tools"
browser_cache="${PLAYWRIGHT_BROWSERS_PATH:-$tools_directory/ms-playwright}"
stable_executable="$tools_directory/retrom-chrome-for-testing"

if [[ ! -x "$node" ]]; then
  echo "pinned Node.js is missing; run make prepare-node" >&2
  exit 1
fi
if [[ ! -f "$playwright_cli" || ! -f "$playwright_manifest" ]]; then
  echo "locked Playwright dependencies are missing; run make web-install" >&2
  exit 1
fi

mkdir -p "$browser_cache" "$tools_directory"
PLAYWRIGHT_BROWSERS_PATH="$browser_cache" \
  "$node" "$playwright_cli" install --no-shell chrome-for-testing

browser_executable="$(
  cd "$repository_root/web"
  PLAYWRIGHT_BROWSERS_PATH="$browser_cache" "$node" --input-type=module -e \
    'import { chromium } from "@playwright/test"; process.stdout.write(chromium.executablePath());'
)"
expected_version="$(
  "$node" -e \
    'const manifest = require(process.argv[1]); const browser = manifest.browsers.find((item) => item.name === "chromium"); if (!browser?.browserVersion) process.exit(1); process.stdout.write(browser.browserVersion);' \
    "$playwright_manifest"
)"

if [[ ! -x "$browser_executable" ]]; then
  echo "Playwright Chrome for Testing executable is missing after installation" >&2
  exit 1
fi
actual_version="$($browser_executable --version | sed -E 's/[[:space:]]+$//')"
if [[ "$actual_version" != "Google Chrome for Testing $expected_version" ]]; then
  echo "unexpected Chrome for Testing version: $actual_version (expected $expected_version)" >&2
  exit 1
fi
if [[ -e "$stable_executable" && ! -L "$stable_executable" ]]; then
  echo "stable Chrome path exists but is not a symbolic link: $stable_executable" >&2
  exit 1
fi

relative_executable="$(realpath --relative-to="$tools_directory" "$browser_executable")"
temporary_link="$tools_directory/.retrom-chrome-for-testing.$$"
cleanup() { rm -f -- "$temporary_link"; }
trap cleanup EXIT
ln -s "$relative_executable" "$temporary_link"
mv -Tf "$temporary_link" "$stable_executable"

RETROM_PREPARED_CHROME="$stable_executable" \
PLAYWRIGHT_BROWSERS_PATH="$browser_cache" \
  "$node" --input-type=module -e '
    import { chromium } from "./web/node_modules/@playwright/test/index.mjs";
    const browser = await chromium.launch({ executablePath: process.env.RETROM_PREPARED_CHROME, headless: true });
    await browser.close();
  '

printf 'e2e_browser=%s\ncache=.cache/tools/ms-playwright\n' "$actual_version"
