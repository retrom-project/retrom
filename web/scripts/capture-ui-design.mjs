import { chromium } from "@playwright/test";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const designRoot = path.resolve(webRoot, "..", "docs", "design");
const documentURL = pathToFileURL(path.join(designRoot, "retrom-ui-review.html"));

const captures = [
  ["retrom-ui-home-4k.png", "home", 3840, 2160],
  ["retrom-ui-library-4k.png", "library", 3840, 2160],
  ["retrom-ui-game-detail.png", "detail", 2560, 1440],
  ["retrom-ui-game-detail-core-override.png", "detail", 2560, 1440, "core-override"],
  ["retrom-ui-saves.png", "saves", 2560, 1440],
  ["retrom-ui-recent-4k.png", "recent", 3840, 2160],
  ["retrom-ui-play.png", "play", 2560, 1440],
  ["retrom-ui-play-portrait.png", "play", 2560, 1440, "portrait"],
  ["retrom-ui-play-4k.png", "play", 3840, 2160],
  ["retrom-ui-play-native-controls.png", "play", 2560, 1440, "native-controls"],
  ["retrom-ui-admin-import-overview-4k.png", "admin-import", 3840, 2160],
  ["retrom-ui-admin-import.png", "admin-import-new", 2560, 1440],
  ["retrom-ui-admin-import-new-4k.png", "admin-import-new", 3840, 2160],
  ["retrom-ui-admin-import-tasks-4k.png", "admin-import-tasks", 3840, 2160],
  ["retrom-ui-admin-review-4k.png", "admin-review", 3840, 2160],
  ["retrom-ui-admin-review-detail-4k.png", "admin-review", 3840, 2160, "review-detail"],
  ["retrom-ui-admin-review-compare-4k.png", "admin-review", 3840, 2160, "review-compare"],
  ["retrom-ui-admin-review-history-4k.png", "admin-review-history", 3840, 2160],
  ["retrom-ui-admin-review-history-detail-4k.png", "admin-review-history", 3840, 2160, "history-detail"],
  ["retrom-ui-admin-games-4k.png", "admin-games", 3840, 2160],
  ["retrom-ui-admin-game-detail-4k.png", "admin-game-detail", 3840, 2160],
  ["retrom-ui-platform-directories.png", "admin-platform-instances", 3840, 2160],
  ["retrom-ui-platform-directory-create.png", "admin-platform-instances", 2560, 1440, "drawer"],
  ["retrom-ui-confirm-dialog.png", "admin-platform-instances", 2560, 1440, "dialog"],
  ["retrom-ui-bios-files.png", "admin-bios", 2560, 1440],
  ["retrom-ui-dat-versions.png", "admin-bios", 2560, 1440, "dats"],
  ["retrom-ui-dat-upload.png", "admin-bios", 2560, 1440, "dat-drawer"],
  ["retrom-ui-dat-diff.png", "admin-bios", 2560, 1440, "dat-diff"]
];

const requestedNames = new Set(process.argv.slice(2));
const selectedCaptures = requestedNames.size
  ? captures.filter(([filename]) => requestedNames.has(filename))
  : captures;
if (selectedCaptures.length !== requestedNames.size) {
  const knownNames = new Set(captures.map(([filename]) => filename));
  const unknownNames = [...requestedNames].filter((filename) => !knownNames.has(filename));
  throw new Error(`unknown design capture: ${unknownNames.join(", ")}`);
}

const browser = await chromium.launch({ headless: true });
try {
  for (const [filename, view, width, height, variant] of selectedCaptures) {
    const page = await browser.newPage({ viewport: { width, height }, deviceScaleFactor: 1, colorScheme: "light" });
    await page.goto(documentURL.href, { waitUntil: "load" });
    const frame = page.frames().find((candidate) => candidate !== page.mainFrame());
    if (!frame) throw new Error(`design iframe missing for ${filename}`);
    await frame.locator("#retrom-ui-review").waitFor({ state: "visible" });
    const clickVisible = async (selector) => {
      const candidates = await frame.locator(selector).all();
      for (const candidate of candidates) {
        if (await candidate.isVisible()) { await candidate.click(); return; }
      }
      throw new Error(`visible design control missing: ${selector}`);
    };
    if (view.startsWith("admin-")) await frame.locator("#rt-mode-button").click();
    if (view === "detail") {
      await clickVisible('[data-open-game="metal"]');
    } else if (view === "play") {
      await clickVisible('[data-quick-start="metal"]');
    } else if (view === "admin-game-detail") {
      await clickVisible('[data-page-target="admin-games"]');
      await clickVisible('[data-page-link="admin-game-detail"]');
    } else if (view !== "home") {
      await clickVisible(`[data-page-target="${view}"], [data-page-link="${view}"]`);
    }
    await frame.locator(`[data-page="${view}"]`).waitFor({ state: "visible" });
    if (["dats", "dat-drawer", "dat-diff"].includes(variant)) await frame.locator('[data-bios-view="dats"]').first().click();
    if (variant === "dat-drawer") await frame.locator("[data-open-runtime-drawer]").click();
    if (variant === "dat-diff") await frame.locator("[data-open-runtime-diff]").first().click();
    if (variant === "drawer") await frame.locator("[data-open-platform-drawer]").click();
    if (variant === "dialog") await frame.locator("[data-preview-core]").first().click();
    if (variant === "portrait") await frame.locator(".rt-player-screen").evaluate((element) => element.classList.add("is-portrait"));
    if (variant === "native-controls") {
      await frame.locator("#rt-player-more").click();
      await frame.locator("[data-open-emulator-controls]").click();
    }
    if (variant === "review-detail" || variant === "review-compare") await frame.locator("[data-review-item]").first().click();
    if (variant === "review-compare") await frame.locator("[data-open-compare]").click();
    if (variant === "history-detail") await frame.locator("[data-open-history]").first().click();
    if (variant === "core-override") {
      await frame.locator("[data-open-detail-runtime]").click();
      await frame.locator("#rt-core-select").selectOption({ label: "MAME 2003 Plus · 可选" });
      await frame.locator("[data-apply-detail-runtime]").click();
      await frame.locator("[data-open-detail-runtime]").click();
    }
    if (view === "play") await page.waitForTimeout(1_400);
    await frame.locator("[data-lucide]").first().waitFor({ state: "attached" });
    await page.screenshot({ path: path.join(designRoot, filename) });
    await page.close();
  }
} finally {
  await browser.close();
}
