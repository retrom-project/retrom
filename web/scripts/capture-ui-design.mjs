import { chromium } from "@playwright/test";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const designRoot = path.resolve(webRoot, "..", "docs", "design");
const documentURL = pathToFileURL(path.join(designRoot, "retrom-ui-review.html"));

const captures = [
  ["retrom-ui-setup.png", "setup", 1280, 800],
  ["retrom-ui-login.png", "login", 1280, 800],
  ["retrom-ui-register.png", "register", 1280, 800],
  ["retrom-ui-reset-password.png", "reset", 1280, 800],
  ["retrom-ui-home-4k.png", "home", 3840, 2160],
  ["retrom-ui-library-4k.png", "library", 3840, 2160],
  ["retrom-ui-game-detail.png", "detail", 2560, 1440],
  ["retrom-ui-game-detail-core-override.png", "detail", 2560, 1440, "core-override"],
  ["retrom-ui-saves.png", "saves", 2560, 1440],
  ["retrom-ui-favorites.png", "favorites", 2560, 1440],
  ["retrom-ui-favorites-folder-manager.png", "favorites", 1280, 800, "folder-manager"],
  ["retrom-ui-favorites-unfavorite-dialog.png", "favorites", 1280, 800, "unfavorite-dialog"],
  ["retrom-ui-recent-4k.png", "recent", 3840, 2160],
  ["retrom-ui-netplay.png", "netplay", 2560, 1440],
  ["retrom-ui-netplay-room.png", "netplay-room", 2560, 1440],
  ["retrom-ui-netplay-player.png", "netplay-player", 2560, 1440],
  ["retrom-ui-account.png", "account", 2560, 1440],
  ["retrom-ui-play.png", "play", 2560, 1440],
  ["retrom-ui-play-portrait.png", "play", 2560, 1440, "portrait"],
  ["retrom-ui-play-4k.png", "play", 3840, 2160],
  ["retrom-ui-play-emulator-controls.png", "play", 2560, 1440, "emulator-controls"],
  ["retrom-ui-play-debug.png", "play", 2560, 1440, "debug"],
  ["retrom-ui-admin-import-overview-4k.png", "admin-import", 3840, 2160],
  ["retrom-ui-admin-import.png", "admin-import-new", 2560, 1440],
  ["retrom-ui-admin-import-new-4k.png", "admin-import-new", 3840, 2160],
  ["retrom-ui-server-import.png", "admin-server-import", 2560, 1440],
  ["retrom-ui-server-import-drawer.png", "admin-server-import", 1280, 800, "server-import-drawer"],
  ["retrom-ui-server-import-detail-4k.png", "admin-server-import-detail", 3840, 2160],
  ["retrom-ui-pegasus-import.png", "admin-server-import", 2560, 1440],
  ["retrom-ui-pegasus-import-drawer.png", "admin-server-import", 1280, 800, "pegasus-import-drawer"],
  ["retrom-ui-pegasus-import-detail-4k.png", "admin-pegasus-import-detail", 3840, 2160],
  ["retrom-ui-admin-import-tasks-4k.png", "admin-import-tasks", 3840, 2160],
  ["retrom-ui-admin-review-4k.png", "admin-review", 3840, 2160],
  ["retrom-ui-admin-review-detail-4k.png", "admin-review", 3840, 2160, "review-detail"],
  ["retrom-ui-admin-review-attachment-4k.png", "admin-review", 3840, 2160, "review-attachment"],
  ["retrom-ui-admin-review-validating-4k.png", "admin-review", 3840, 2160, "review-validating"],
  ["retrom-ui-admin-review-ready-4k.png", "admin-review", 3840, 2160, "review-ready"],
  ["retrom-ui-admin-review-override-4k.png", "admin-review", 3840, 2160, "review-override"],
  ["retrom-ui-admin-review-compare-4k.png", "admin-review", 3840, 2160, "review-compare"],
  ["retrom-ui-admin-review-history-4k.png", "admin-review-history", 3840, 2160],
  ["retrom-ui-admin-review-history-detail-4k.png", "admin-review-history", 3840, 2160, "history-detail"],
  ["retrom-ui-admin-games-4k.png", "admin-games", 3840, 2160],
  ["retrom-ui-admin-game-detail-4k.png", "admin-game-detail", 3840, 2160],
  ["retrom-ui-admin-tags-4k.png", "admin-tags", 3840, 2160],
  ["retrom-ui-admin-tags-mobile.png", "admin-tags", 390, 844],
  ["retrom-ui-admin-tag-drawer.png", "admin-tags", 1280, 800, "tag-drawer"],
  ["retrom-ui-platform-directories.png", "admin-platform-instances", 3840, 2160],
  ["retrom-ui-platform-directory-create.png", "admin-platform-instances", 2560, 1440, "drawer"],
  ["retrom-ui-confirm-dialog.png", "admin-platform-instances", 2560, 1440, "dialog"],
  ["retrom-ui-admin-users-4k.png", "admin-users", 3840, 2160],
  ["retrom-ui-admin-user-drawer.png", "admin-users", 2560, 1440, "user-drawer"],
  ["retrom-ui-admin-invitation-result.png", "admin-users", 2560, 1440, "invitation-result"],
  ["retrom-ui-bios-files.png", "admin-bios", 2560, 1440],
  ["retrom-ui-bios-entry-compare.png", "admin-bios", 2560, 1440, "bios-entries"],
  ["retrom-ui-dat-versions.png", "admin-bios", 2560, 1440, "dats"],
  ["retrom-ui-dat-upload.png", "admin-bios", 2560, 1440, "dat-drawer"],
  ["retrom-ui-dat-diff.png", "admin-bios", 2560, 1440, "dat-diff"],
  ["retrom-ui-home-mobile.png", "home", 390, 844],
  ["retrom-ui-library-mobile.png", "library", 390, 844],
  ["retrom-ui-game-detail-mobile.png", "detail", 390, 844],
  ["retrom-ui-saves-mobile.png", "saves", 390, 844],
  ["retrom-ui-favorites-mobile.png", "favorites", 390, 844],
  ["retrom-ui-netplay-room-mobile.png", "netplay-room", 390, 844],
  ["retrom-ui-admin-review-mobile.png", "admin-review", 390, 844],
  ["retrom-ui-admin-review-detail-mobile.png", "admin-review", 390, 844, "review-detail"],
  ["retrom-ui-play-portrait-mobile.png", "play", 390, 844, "mobile-portrait"],
  ["retrom-ui-play-landscape-mobile.png", "play", 844, 390]
];

const requestedNames = new Set(process.argv.slice(2));
const selectedCaptures = requestedNames.size
  ? captures.filter(([filename]) => requestedNames.has(filename))
  : captures;
if (requestedNames.size && selectedCaptures.length !== requestedNames.size) {
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
    const activate = async (selector) => {
      const target = frame.locator(selector).first();
      if (!await target.count()) throw new Error(`design control missing: ${selector}`);
      if (await target.isVisible()) await target.click();
      else await target.evaluate((element) => element.click());
    };
    if (["setup", "login", "register", "reset"].includes(view)) {
      await frame.locator(`[data-review-scene="${view}"]`).click();
    } else if (view.startsWith("admin-")) {
      await frame.locator("#rt-mode-button").evaluate((element) => element.click());
    }
    if (view === "account") {
      await frame.locator('[data-review-scene="account"]').click();
    } else if (view === "detail") {
      await clickVisible('[data-open-game="metal"]');
    } else if (view === "play") {
      await clickVisible('[data-quick-start="metal"]');
    } else if (view === "admin-game-detail") {
      await activate('[data-page-target="admin-games"]');
      await activate('[data-page-link="admin-game-detail"]');
    } else if (view === "admin-server-import-detail") {
      await activate('[data-page-target="admin-server-import"]');
      await activate('[data-page-link="admin-server-import-detail"]');
    } else if (view === "admin-pegasus-import-detail") {
      await activate('[data-page-target="admin-server-import"]');
      await activate('[data-page-link="admin-pegasus-import-detail"]');
    } else if (view === "netplay-room") {
      await frame.locator('[data-review-scene="netplay-room"]').click();
    } else if (view === "netplay-player") {
      await frame.locator('[data-review-scene="netplay-player"]').click();
    } else if (!["home", "setup", "login", "register", "reset"].includes(view)) {
      if (view.startsWith("admin-")) await activate(`[data-page-target="${view}"], [data-page-link="${view}"]`);
      else await clickVisible(`[data-page-target="${view}"], [data-page-link="${view}"]`);
    }
    const viewSelector = ["setup", "login", "register", "reset"].includes(view)
      ? `[data-auth-page="${view}"]`
      : `[data-page="${view}"]`;
    await frame.locator(viewSelector).waitFor({ state: "visible" });
    await frame.locator(".rt-review-scenes").evaluate((element) => { element.hidden = true; });
    if (["dats", "dat-drawer", "dat-diff"].includes(variant)) await frame.locator('[data-bios-view="dats"]').first().click();
    if (variant === "bios-entries") await frame.locator("[data-open-bios-entries]").click();
    if (variant === "dat-drawer") await frame.locator("[data-open-runtime-drawer]").click();
    if (variant === "dat-diff") await frame.locator("[data-open-runtime-diff]").first().click();
    if (variant === "server-import-drawer") await frame.locator("[data-open-server-import-drawer]").click();
    if (variant === "pegasus-import-drawer") await frame.locator("[data-open-pegasus-drawer]").click();
    if (variant === "drawer") await frame.locator("[data-open-platform-drawer]").click();
    if (variant === "dialog") await frame.locator("[data-preview-core]").first().click();
    if (variant === "user-drawer") await frame.locator("[data-open-user-drawer]").nth(1).click();
    if (variant === "tag-drawer") await frame.locator("[data-open-tag-drawer]").first().click();
    if (variant === "invitation-result") {
      await frame.locator("[data-open-invite-drawer]").click();
      await frame.locator("[data-create-invitation]").click();
    }
    if (variant === "portrait") await frame.locator('[data-page="play"] .rt-player-screen').evaluate((element) => element.classList.add("is-portrait"));
    if (variant === "mobile-portrait") await frame.locator('[data-page="play"] .rt-player-shell').evaluate((element) => element.classList.add("is-mobile-portrait"));
    if (variant === "emulator-controls") {
      await frame.locator("#rt-player-more").click();
      await frame.locator("[data-open-emulator-controls]").click();
    }
    if (variant === "debug") await frame.locator("#rt-player-debug").click();
    if (["review-detail", "review-attachment", "review-validating", "review-ready", "review-override", "review-compare"].includes(variant)) await frame.locator("[data-review-item]").first().click();
    if (variant === "review-attachment") await frame.locator("[data-open-disc-drawer]").click();
    if (variant === "review-validating") {
      await frame.locator("[data-open-disc-drawer]").click();
      await frame.locator("[data-queue-disc-validation]").click();
    }
    if (variant === "review-ready") await frame.locator("#retrom-ui-review").evaluate((element) => element.dispatchEvent(new CustomEvent("retrom:set-disc-state", { detail: "ready" })));
    if (variant === "review-override") await frame.locator("#retrom-ui-review").evaluate((element) => element.dispatchEvent(new CustomEvent("retrom:set-disc-state", { detail: "override" })));
    if (variant === "review-compare") await frame.locator("[data-open-compare]").click();
    if (variant === "history-detail") await frame.locator("[data-open-history]").first().click();
    if (variant === "core-override") {
      await frame.locator("[data-open-detail-runtime]").click();
      await frame.locator("#rt-core-select").selectOption({ label: "MAME 2003 Plus · 可选" });
      await frame.locator("[data-apply-detail-runtime]").click();
      await frame.locator("[data-open-detail-runtime]").click();
    }
    if (variant === "folder-manager") await frame.locator("[data-favorite-manage]").first().click();
    if (variant === "unfavorite-dialog") await frame.locator("[data-favorite-heart]").first().click();
    if (view === "play") {
      await page.waitForTimeout(1_400);
      await frame.locator("#rt-player-game").evaluate((element) => { element.textContent = "NOeL 3"; });
      await frame.locator("#rt-player-core").evaluate((element) => { element.textContent = "Yabause · Saturn"; });
    }
    await frame.locator("[data-lucide]").first().waitFor({ state: "attached" });
    await page.screenshot({ path: path.join(designRoot, filename) });
    await page.close();
  }
} finally {
  await browser.close();
}
