import { expect, test, type Page } from "@playwright/test";

// Uses an accessible published game from the product fixture, without changing it.
test("ACC-UI-005 detail poster fills its column without footer descriptions", async ({ page }, testInfo) => {
  test.setTimeout(90_000);
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const login = await page.request.post("/api/v1/auth/login", { headers: { Origin: origin }, data: { username: "test", password: "test" } });
  expect(login.ok()).toBe(true);
  const games = await page.request.get("/api/v1/games?limit=1");
  expect(games.ok()).toBe(true);
  const gameId: string = (await games.json()).items[0].gameId;
  const sizes = testInfo.project.name === "chrome-1280" ? [[1280, 800], [1920, 950], [390, 844]] : [[2560, 1360], [2560, 1440], [3840, 2160]];
  for (const [width, height] of sizes) {
    await page.setViewportSize({ width: width!, height: height! });
    await page.goto("/");
    const homeLink = page.locator(".home-section-head > a").first();
    if (width! >= 1280) {await expect(homeLink).toBeVisible();}
    await page.goto(`/games/${gameId}`);
    await expect(page.locator(".game-detail-poster")).toBeVisible();
    const labels = (await page.locator(".game-detail-eyebrow").innerText()).split(" · ");
    expect(new Set(labels).size).toBe(labels.length);
    await expect(page.locator(".game-detail-poster-caption")).toHaveCount(0);
    await expect(page.locator(".game-detail-media [aria-live]")).toHaveClass("sr-only");
    const layout = await page.locator(".game-detail-poster").evaluate((poster) => {
      const cover = poster.getBoundingClientRect();
      const shell = poster.closest(".game-detail-poster-shell")!.getBoundingClientRect();
      return { topGap: cover.top - shell.top, bottomGap: shell.bottom - cover.bottom, width: document.documentElement.scrollWidth, viewportWidth: innerWidth };
    });
    expect(Math.abs(layout.topGap)).toBeLessThanOrEqual(1);
    expect(Math.abs(layout.bottomGap)).toBeLessThanOrEqual(1);
    expect(layout.width).toBeLessThanOrEqual(layout.viewportWidth);
    if (width! >= 1280) {
      await expectDetailAlignment(page);
      await expectDetailActions(page);
      await expect(page.locator(".game-detail-breadcrumb a")).toHaveCSS("font-size", "14px");
      await expect(page.locator(".launch-panel-head h2")).toHaveCSS("font-size", "20px");
      await expect(page.locator(".launch-runtime-row button")).toHaveCSS("font-size", "14px");
      if (width === sizes[0]![0] && height === sizes[0]![1]) {
        const initialHeight = await page.locator(".game-detail-hero").evaluate((element) => element.getBoundingClientRect().height);
        await page.locator(".game-detail-description").evaluate((description) => {
          description.querySelector("p")!.textContent = Array.from({ length: 12 }, (_, index) => `第 ${index + 1} 段：这是一段用于验证超长简介滚动的游戏简介，包含玩法、故事背景与操作说明。`).join("\n\n");
        });
        await expectDetailAlignment(page);
        const finalHeight = await page.locator(".game-detail-hero").evaluate((element) => element.getBoundingClientRect().height);
        expect(Math.abs(finalHeight - initialHeight)).toBeLessThanOrEqual(1);
      }
    }
  }
});

async function expectDetailAlignment(page: Page) {
  const layout = await page.evaluate(() => {
    const bounds = (selector: string) => document.querySelector(selector)!.getBoundingClientRect();
    const cover = bounds(".game-detail-poster");
    const hero = bounds(".game-detail-hero");
    const panel = bounds(".launch-panel");
    const eyebrow = bounds(".game-detail-eyebrow");
    const title = bounds(".game-detail-main h1");
    const description = bounds(".game-detail-description");
    const playtime = bounds(".game-detail-playtime");
    return { coverRatio: cover.width / cover.height, heroHeight: hero.height, titleTop: eyebrow.top - panel.top, titleBeforeDescription: title.bottom <= description.top, footerBottom: playtime.bottom - panel.bottom, descriptionBottom: playtime.top - description.bottom };
  });
  for (const delta of [layout.titleTop, layout.footerBottom]) {
    expect(Math.abs(delta)).toBeLessThanOrEqual(1);
  }
  expect(layout.coverRatio).toBeCloseTo(3 / 4, 2);
  expect(layout.heroHeight).toBeLessThanOrEqual(440);
  expect(layout.titleBeforeDescription).toBe(true);
  expect(layout.descriptionBottom).toBeGreaterThanOrEqual(14);
  expect(layout.descriptionBottom).toBeLessThanOrEqual(18);
}

async function expectDetailActions(page: Page) {
  await expect(page.locator(".launch-panel-head .status, .launch-runtime-status")).toHaveCount(0);
  const title = page.locator(".game-detail-title-row");
  const heart = title.locator(".favorite-heart");
  await expect(heart).toBeVisible();
  await expect(heart).toHaveText("");
  await expect(page.locator(".game-detail-main .favorite-manage")).toHaveCount(0);
  const layout = await page.evaluate(() => {
    const rect = (selector: string) => document.querySelector(selector)!.getBoundingClientRect();
    const heart = rect(".game-detail-title-row .favorite-heart");
    const title = rect(".game-detail-main h1");
    const panel = document.querySelector(".launch-panel")!;
    const action = panel.querySelector(":scope > .button")!.getBoundingClientRect();
    const bounds = panel.getBoundingClientRect();
    const saved = panel.querySelector(".launch-quick-save")?.getBoundingClientRect();
    const screenshot = panel.querySelector(".launch-quick-save > div:first-child")?.getBoundingClientRect();
    const runtime = panel.querySelector(".launch-runtime-row")!.getBoundingClientRect();
    const core = rect(".launch-runtime-choice > div");
    const change = rect(".launch-runtime-row button");
    return { heartWidth: heart.width, heartHeight: heart.height, beforeTitle: heart.right <= title.left, radius: getComputedStyle(document.querySelector(".game-detail-title-row .favorite-heart")!).borderRadius, coreCenter: core.y + core.height / 2, changeCenter: change.y + change.height / 2, actionToDivider: runtime.top - action.bottom, runtimeBottom: bounds.bottom - runtime.bottom, screenshotWidth: screenshot?.width, screenshotRatio: screenshot ? screenshot.width / screenshot.height : undefined, padding: parseFloat(getComputedStyle(panel).paddingBottom), savedHeight: saved?.height };
  });
  expect(Math.abs(layout.coreCenter - layout.changeCenter)).toBeLessThanOrEqual(1);
  expect(layout.heartWidth).toBe(38);
  expect(layout.heartHeight).toBe(38);
  expect(layout.radius).toBe("50%");
  expect(layout.beforeTitle).toBe(true);
  expect(layout.actionToDivider).toBeCloseTo(12, 0);
  expect(Math.abs(layout.runtimeBottom - layout.padding)).toBeLessThanOrEqual(1);
  if (layout.savedHeight !== undefined) {
    expect(layout.savedHeight).toBeGreaterThanOrEqual(136);
    expect(layout.screenshotWidth).toBe(112);
    await expect(page.locator(".launch-quick-save")).toHaveCSS("border-radius", "8px");
    await expect(page.locator(".launch-quick-save")).toHaveCSS("border-top-width", "1px");
    expect(layout.screenshotRatio).toBeCloseTo(16 / 9, 2);
  }
}

test("ACC-UI-005 launch panel explains an empty save history", async ({ page }) => {
  test.setTimeout(60_000);
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const login = await page.request.post("/api/v1/auth/login", { headers: { Origin: origin }, data: { username: "test", password: "test" } });
  expect(login.ok()).toBe(true);
  const response = await page.request.get("/api/v1/games?limit=10");
  expect(response.ok()).toBe(true);
  const games: { items: { gameId: string }[] } = await response.json();
  let gameId = "";
  for (const game of games.items) {
    const saves = await page.request.get(`/api/v1/saves?gameId=${game.gameId}&limit=1`);
    expect(saves.ok()).toBe(true);
    if ((await saves.json()).items.length === 0) {gameId = game.gameId; break;}
  }
  expect(gameId, "Product fixture needs a published game without saves").not.toBe("");
  await page.goto(`/games/${gameId}`);
  const panel = page.getByRole("complementary", { name: "启动游戏" });
  await expect(panel.getByText("还没有可继续的存档")).toBeVisible();
  await expect(panel.getByRole("button", { name: "开始游戏", exact: true })).toBeEnabled();
  await expect(panel.getByRole("button", { name: "从存档继续" })).toHaveCount(0);
  const gap = await panel.evaluate((element) => element.querySelector(":scope > .button")!.getBoundingClientRect().top - element.querySelector(".launch-empty-save")!.getBoundingClientRect().bottom);
  expect(gap).toBeGreaterThanOrEqual(7);
  await expectDetailAlignment(page);
  await expectDetailActions(page);
  await panel.getByRole("button", { name: "更换", exact: true }).click();
  await expect(page.getByRole("alertdialog", { name: "更换运行方式" })).toBeVisible();
  await page.getByRole("button", { name: "取消", exact: true }).click();
});
