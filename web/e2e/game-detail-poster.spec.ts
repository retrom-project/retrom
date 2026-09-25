import { expect, test, type Page } from "@playwright/test";

async function login(page: Page) {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  expect((await page.request.post("/api/v1/auth/login", { headers: { Origin: origin }, data: { username: "test", password: "test" } })).ok()).toBe(true);
}

test("ACC-UI-005 detail separates static cover, launch, preview and reading areas", async ({ page }, testInfo) => {
  test.setTimeout(90_000);
  await login(page);
  const games = await (await page.request.get("/api/v1/games?limit=1")).json();
  const sizes = testInfo.project.name === "chrome-1280" ? [[1280, 800], [1920, 950], [390, 844]] : [[2560, 1360], [2560, 1440], [3840, 2160]];
  for (const [width, height] of sizes) {
    await page.setViewportSize({ width, height });
    await page.goto(`/games/${games.items[0].gameId}`);
    await expect(page.locator(".game-detail-poster")).toBeVisible();
    await expect(page.locator(".game-detail-poster video")).toHaveCount(0);
    const labels = (await page.locator(".game-detail-eyebrow").innerText()).split(" · ");
    expect(new Set(labels).size).toBe(labels.length);
    const layout = await page.evaluate(() => {
      const rect = (selector: string) => document.querySelector(selector)!.getBoundingClientRect();
      const cover = rect(".game-detail-poster"), hero = rect(".game-detail-hero"), about = rect(".game-detail-overview");
      const title = rect(".game-detail-title-row h1"), heart = rect(".favorite-heart");
      return { ratio: cover.width / cover.height, contentWidth: rect(".game-detail-content").width, heroBottom: hero.bottom, aboutTop: about.top, heartAfterTitle: heart.left >= title.right, heartWidth: heart.width, overflow: document.documentElement.scrollWidth > innerWidth };
    });
    expect(layout.ratio).toBeCloseTo(.75, 2);
    expect(layout.contentWidth).toBeLessThanOrEqual(1800);
    expect(layout.aboutTop).toBeGreaterThan(layout.heroBottom);
    expect(layout.overflow).toBe(false);
    expect(layout.heartWidth).toBe(38);
    await expect(page.locator(".game-detail-description")).toHaveCSS("overflow-y", "visible");
    if (width >= 1280) {
      expect(layout.heartAfterTitle).toBe(true);
      await expect(page.locator(".game-detail-main .launch-actions .button").first()).toBeVisible();
      const actionLayout = await page.evaluate(() => {
        const title = document.querySelector(".game-detail-title-row")!.getBoundingClientRect();
        const actions = document.querySelector(".launch-actions")!.getBoundingClientRect();
        const runtime = document.querySelector(".launch-runtime-row")!.getBoundingClientRect();
        return { titleBottom: title.bottom, actionsTop: actions.top, actionsBottom: actions.bottom, runtimeTop: runtime.top };
      });
      expect(actionLayout.actionsTop).toBeGreaterThan(actionLayout.titleBottom);
      expect(actionLayout.runtimeTop).toBeGreaterThan(actionLayout.actionsBottom);
    } else {
      await expect(page.getByRole("button", { name: "启动选项" })).toBeVisible();
      await expect(page.locator(".mobile-launch-dock .button")).toBeVisible();
    }
    if (width === 2560 || width === 390) {await page.screenshot({ path: testInfo.outputPath(`detail-${width}x${height}.png`), fullPage: true });}
  }
});

test("ACC-UI-005 empty history has a compact start hint and preserves core selection", async ({ page }) => {
  await login(page);
  const games = await (await page.request.get("/api/v1/games?limit=100")).json();
  let gameId = "";
  for (const game of games.items) {
    const saves = await (await page.request.get(`/api/v1/saves?gameId=${game.gameId}&limit=1`)).json();
    if (!saves.items.length) {gameId = game.gameId; break;}
  }
  expect(gameId, "fixture includes a published game without saves").not.toBe("");
  await page.goto(`/games/${gameId}`);
  const panel = page.getByRole("complementary", { name: "启动游戏" });
  await expect(panel.locator(".launch-hint")).toHaveText("本次将从游戏开头启动。");
  await expect(panel.getByRole("button", { name: "开始游戏", exact: true })).toBeEnabled();
  await expect(panel.getByRole("button", { name: "从存档继续" })).toHaveCount(0);
  await expect(page.locator(".game-detail-saves-empty")).toContainText("还没有存档");
  const before = await page.locator(".game-detail-hero").boundingBox();
  await panel.getByRole("button", { name: "更换", exact: true }).click();
  await expect(page.getByRole("alertdialog", { name: "更换运行方式" })).toBeVisible();
  expect(await page.locator(".game-detail-hero").boundingBox()).toEqual(before);
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await expect(panel.getByRole("button", { name: "更换", exact: true })).toBeFocused();
});
