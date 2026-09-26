import { expect, test, type Page } from "@playwright/test";

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

test("ACC-UI-005 detail description uses natural page flow below a stable hero", async ({ page }, testInfo) => {
  await login(page);
  const games = await (await page.request.get("/api/v1/games?limit=1")).json();
  const gameId = process.env.RETROM_REVIEW_GAME_ID ?? games.items[0].gameId;
  const game = await (await page.request.get(`/api/v1/games/${gameId}`)).json();
  await page.goto(`/games/${gameId}`);
  const description = page.locator(".game-detail-description");
  await expect(description).toBeVisible();
  const expand = description.getByRole("button", { name: "展开完整简介" });
  const before = await page.locator(".game-detail-hero").boundingBox();
  if (Array.from(game.description.trim()).length > 320) {
    await expect(expand).toHaveAttribute("aria-expanded", "false");
    await expand.click();
    await expect(description.getByRole("button", { name: "收起简介" })).toHaveAttribute("aria-expanded", "true");
  }
  await expect(description.locator("p")).toHaveText(game.description.trim() ? game.description : "尚未填写游戏简介。");
  await expect(description).toHaveCSS("overflow-y", "visible");
  await description.locator("p").evaluate((element) => {element.textContent = "完整的游戏简介，不截断文字。\n\n".repeat(100);});
  const flow = await description.evaluate((element) => ({ height: element.clientHeight, scroll: element.scrollHeight, bottom: element.getBoundingClientRect().bottom, savesTop: document.querySelector(".game-detail-saves")!.getBoundingClientRect().top }));
  expect(flow.scroll).toBeLessThanOrEqual(flow.height + 1);
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
