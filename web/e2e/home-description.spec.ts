import { expect, test, type Page } from "@playwright/test";

test("ACC-UI-005 home description stays between aligned information and launch controls", async ({ page }, testInfo) => {
  test.setTimeout(60_000);
  await login(page);
  const response = await page.request.get("/api/v1/home");
  expect(response.ok()).toBe(true);
  const home = await response.json();
  expect(typeof home.featuredGame.description).toBe("string");
  const sizes = testInfo.project.name === "chrome-1280" ? [[1280, 800], [1920, 950], [2086, 920]] : [[2560, 1360], [2560, 1440], [3840, 2160]];
  for (const [width, height] of sizes) {
    await page.setViewportSize({ width: width!, height: height! });
    await page.goto("/");
    await expect(page.locator(".home-featured-copy")).toBeVisible();
    const summary = page.locator(".home-featured-description");
    if (home.featuredGame.description.trim()) {
      expect(Array.from(await summary.innerText()).length).toBeLessThanOrEqual(160);
    }
    const original = await homeBounds(page);
    await page.locator(".home-featured-copy").evaluate((copy) => {
      let description = copy.querySelector(".home-featured-description");
      if (!description) {
        description = document.createElement("div");
        description.className = "home-featured-description";
        copy.insertBefore(description, copy.querySelector(".home-featured-actions"));
      }
      const text = document.createElement("p");
      text.textContent = "这是很长的游戏简介，用于检查布局。".repeat(1000);
      description.replaceChildren(text);
    });
    const updated = await homeBounds(page);
    expect(updated).toEqual(original);
    expect(Math.abs(updated.detailsTop - updated.coverTop)).toBeLessThanOrEqual(1);
    expect(Math.abs(updated.actionsBottom - updated.coverBottom)).toBeLessThanOrEqual(1);
    const lineLimit = await summary.locator("p").evaluate((element) => Number(getComputedStyle(element).webkitLineClamp));
    expect(lineLimit).toBeGreaterThanOrEqual(1);
    expect(lineLimit).toBeLessThanOrEqual(4);
    if (width === 2560 && height === 1360) {await page.locator(".home-featured-panel").screenshot({ path: testInfo.outputPath("home-description.png"), scale: "css" });}
  }
});

test("ACC-UI-005 full detail description scrolls without growing the hero", async ({ page }, testInfo) => {
  test.setTimeout(60_000);
  await login(page);
  const games = await (await page.request.get("/api/v1/games?limit=1")).json();
  const gameId = process.env.RETROM_REVIEW_GAME_ID ?? games.items[0].gameId;
  const game = await (await page.request.get(`/api/v1/games/${gameId}`)).json();
  await page.goto(`/games/${gameId}`);
  const description = page.getByRole("region", { name: "游戏简介", exact: true });
  await expect(description).toBeVisible();
  expect(await description.textContent()).toBe(game.description.trim() ? game.description : "尚未填写游戏简介。");
  const before = await page.locator(".game-detail-hero").boundingBox();
  await description.locator("p").evaluate((element) => {element.textContent = "完整的游戏简介，不截断文字。\n\n".repeat(1000);});
  expect(await page.locator(".game-detail-hero").boundingBox()).toEqual(before);
  await expect(description).toHaveCSS("scrollbar-width", "thin");
  await expect(description).toHaveCSS("scrollbar-color", "rgba(0, 0, 0, 0) rgba(0, 0, 0, 0)");
  await description.hover();
  await page.mouse.wheel(0, 140);
  await expect(description).toHaveClass(/is-scrolling/);
  await expect(description).toHaveCSS("scrollbar-color", "rgb(174, 181, 194) rgba(0, 0, 0, 0)");
  await expect.poll(() => description.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
  await expect(description).not.toHaveClass(/is-scrolling/);
  await description.focus();
  await page.keyboard.press("Control+End");
  await expect.poll(() => description.evaluate((element) => Math.abs(element.scrollHeight - element.clientHeight - element.scrollTop))).toBeLessThanOrEqual(1);
  expect(await page.locator(".game-detail-hero").boundingBox()).toEqual(before);
  await description.locator("p").evaluate((element, text) => {element.textContent = text;}, game.description || "尚未填写游戏简介。");
  await description.evaluate((element) => {element.scrollTop = 0; element.blur();});
  await expect(description).not.toHaveClass(/is-scrolling/);
  await page.locator(".game-detail-hero").screenshot({ path: testInfo.outputPath("detail-description.png"), scale: "css" });
});

async function login(page: Page) {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const response = await page.request.post("/api/v1/auth/login", { headers: { Origin: origin }, data: { username: "test", password: "test" } });
  expect(response.ok()).toBe(true);
}

async function homeBounds(page: Page) {
  return page.evaluate(() => {
    const rect = (selector: string) => document.querySelector(selector)!.getBoundingClientRect();
    return { height: rect(".home-featured-media").height, coverTop: rect(".home-featured-cover").top, coverBottom: rect(".home-featured-cover").bottom, detailsTop: rect(".home-featured-details").top, actionsBottom: rect(".home-featured-actions").bottom };
  });
}
