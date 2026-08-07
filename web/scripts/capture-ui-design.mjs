import { chromium } from "@playwright/test";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const designRoot = path.resolve(webRoot, "..", "docs", "design");
const documentURL = pathToFileURL(path.join(designRoot, "retrom-ui-review.html"));

const captures = [
  ["retrom-ui-library-4k.png", "library", 3840, 2160],
  ["retrom-ui-game-detail.png", "detail", 2560, 1440],
  ["retrom-ui-saves.png", "saves", 2560, 1440],
  ["retrom-ui-play.png", "play", 2560, 1440],
  ["retrom-ui-play-4k.png", "play", 3840, 2160],
  ["retrom-ui-admin-import-overview-4k.png", "admin-import", 3840, 2160],
  ["retrom-ui-admin-import.png", "admin-import-new", 2560, 1440],
  ["retrom-ui-admin-import-new-4k.png", "admin-import-new", 3840, 2160],
  ["retrom-ui-admin-import-tasks-4k.png", "admin-import-tasks", 3840, 2160],
  ["retrom-ui-admin-review-4k.png", "admin-review", 3840, 2160],
  ["retrom-ui-admin-review-history-4k.png", "admin-review-history", 3840, 2160],
  ["retrom-ui-admin-games-4k.png", "admin-games", 3840, 2160],
  ["retrom-ui-admin-game-detail-4k.png", "admin-game-detail", 3840, 2160],
  ["retrom-ui-platform-directories.png", "admin-platform-instances", 3840, 2160],
  ["retrom-ui-bios-files.png", "admin-bios", 2560, 1440],
  ["retrom-ui-dat-versions.png", "admin-bios", 2560, 1440, "dats"]
];

const browser = await chromium.launch({ headless: true });
try {
  for (const [filename, view, width, height, biosView] of captures) {
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
    if (biosView === "dats") await frame.getByRole("button", { name: "Arcade DAT 版本" }).click();
    await frame.locator("[data-lucide]").first().waitFor({ state: "attached" });
    await page.screenshot({ path: path.join(designRoot, filename) });
    await page.close();
  }
} finally {
  await browser.close();
}
