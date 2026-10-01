import { expect, test, type Page } from "@playwright/test";
import { seedHomeState, uiLayoutState } from "./ui-layout-state";

test("ACC-UI-005 home keeps long titles and launch controls within its hero", async ({ page }, testInfo) => {
  await login(page);
  const sizes = testInfo.project.name === "chrome-1280" ? [[1280, 800], [1920, 950], [2086, 920]] : [[2560, 1360], [2560, 1440], [3840, 2160]];
  for (const [width, height] of sizes) {
    await page.setViewportSize({ width: width!, height: height! });
    await page.goto("/");
    await expect(page.locator(".home-featured-copy")).toBeVisible();
    await expect(page.locator(".home-featured-description")).toHaveCount(0);
    await page.locator(".home-featured-details h2").evaluate((heading) => {
      heading.textContent = "一个很长的游戏标题 The Legend of a Long Adventure";
    });
    const geometry = await page.locator(".home-featured-panel").evaluate((element) => {
      const panel = element.getBoundingClientRect();
      const title = element.querySelector("h2")!.getBoundingClientRect();
      const actions = element.querySelector(".home-featured-actions")!.getBoundingClientRect();
      return { titleFits: title.left >= panel.left && title.right <= panel.right, actionsFit: actions.bottom <= panel.bottom && actions.right <= panel.right, gap: actions.top - title.bottom };
    });
    expect(geometry.titleFits).toBe(true);
    expect(geometry.actionsFit).toBe(true);
    expect(geometry.gap).toBeGreaterThan(0);
    if (width === 2560 && height === 1360) {await page.locator(".home-featured-panel").screenshot({ path: testInfo.outputPath("home-long-title.png"), scale: "css" });}
  }
});

test("ACC-UI-005 detail description shows full text inside a bounded scroll area below a stable hero", async ({ page }, testInfo) => {
  await login(page);
  const games = await (await page.request.get("/api/v1/games?limit=1")).json();
  const gameId = process.env.RETROM_REVIEW_GAME_ID ?? games.items[0].gameId;
  const game = await (await page.request.get(`/api/v1/games/${gameId}`)).json();
  await page.goto(`/games/${gameId}`);
  const description = page.locator(".game-detail-description");
  await expect(description).toBeVisible();
  const before = await page.locator(".game-detail-hero").boundingBox();
  const beforeDescription = await description.boundingBox();
  await expect(description.getByRole("button")).toHaveCount(0);
  await expect(description.locator("p")).toHaveText(game.description.trim() ? game.description : "尚未填写游戏简介。");
  await expect(description).toHaveCSS("overflow-y", "auto");
  await description.locator("p").evaluate((element) => {element.textContent = "完整的游戏简介，不截断文字。\n\n".repeat(100);});
  const flow = await description.evaluate((element) => ({ height: element.clientHeight, scroll: element.scrollHeight, bottom: element.getBoundingClientRect().bottom, savesTop: document.querySelector(".game-detail-saves")!.getBoundingClientRect().top }));
  expect(flow.scroll).toBeGreaterThan(flow.height);
  expect(await description.boundingBox()).toEqual(beforeDescription);
  await description.evaluate(element => {element.scrollTop = element.scrollHeight;});
  expect(await description.evaluate(element => element.scrollTop)).toBeGreaterThan(0);
  expect(flow.savesTop).toBeGreaterThan(flow.bottom);
  expect(await page.locator(".game-detail-hero").boundingBox()).toEqual(before);
  await page.reload();
  await page.screenshot({ path: testInfo.outputPath("detail-description.png") });
});

async function login(page: Page) {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const response = await page.request.post("/api/v1/auth/login", { headers: { Origin: origin }, data: { username: "test", password: "test" } });
  expect(response.ok()).toBe(true);
}

test("ACC-UI-005 failed recent launch uses a centered three-second toast without moving the row", async ({ page }, testInfo) => {
  uiLayoutState("isolate");
  try {
    seedHomeState("played");
    await login(page);
    await page.route("**/api/v1/launches", route => route.fulfill({
      status: 422, contentType: "application/json",
      body: JSON.stringify({ error: { code: "LAUNCH_BLOCKED", message: "当前游戏或核心无法启动", details: {}, requestId: "01980000-0000-7000-8000-000000000001" } }),
    }));
    await page.goto("/recent");
    const row = page.locator(".recent-history-row").first();
    const button = row.getByRole("button", { name: "再玩一次", exact: true });
    await expect(button).toBeEnabled();
    const layout = async () => {
      await row.evaluate(async element => {
        await Promise.all(element.getAnimations({ subtree: true })
          .filter(animation => animation instanceof CSSTransition)
          .map(animation => animation.finished.catch(() => undefined)));
      });
      return { row: await row.boundingBox(), button: await button.boundingBox() };
    };
    await button.hover();
    const before = await layout();
    await button.click();
    const toast = page.locator(".app-toast[role=alert]");
    await expect(toast).toHaveText("当前游戏或核心无法启动");
    await expect(toast.getByRole("button")).toHaveCount(0);
    await expect(row.getByRole("alert")).toHaveCount(0);
    await expect(button).toBeEnabled();
    expect(await layout()).toEqual(before);
    const geometry = await toast.evaluate(element => {
      const rect = element.getBoundingClientRect();
      return { centered: Math.abs(rect.x + rect.width / 2 - innerWidth / 2), top: rect.top, position: getComputedStyle(element).position, parent: element.parentElement?.tagName };
    });
    expect(geometry.centered).toBeLessThan(1);
    expect(geometry.top).toBeLessThan(60);
    expect(geometry.position).toBe("fixed");
    expect(geometry.parent).toBe("BODY");
    await page.screenshot({ path: testInfo.outputPath("recent-launch-error-toast.png") });
    await expect(toast).toHaveCount(0, { timeout: 4_000 });
    expect(await layout()).toEqual(before);
  } finally { uiLayoutState("restore"); }
});
