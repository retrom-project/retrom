import { expect, test } from "@playwright/test";

test("ACC-UI-005 library cards align without exposing filter tags", async ({ page }, testInfo) => {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const login = await page.request.post("/api/v1/auth/login", { headers: { Origin: origin }, data: { username: "test", password: "test" } });
  expect(login.ok()).toBe(true);
  await page.goto("/library");
  const cards = page.locator(".library-game-card");
  await expect(cards.nth(1)).toBeVisible();
  await expect(cards.locator(".tag-chips")).toHaveCount(0);
  const poster = page.locator(".library-poster").first();
  await expect(poster).toBeVisible();
  await poster.locator("strong").evaluate((element) => {element.textContent = "短标题";});
  const before = await poster.locator("small, :scope > span").evaluateAll((elements) => elements.map((element) => element.getBoundingClientRect().y));
  await poster.locator("strong").evaluate((element) => {element.textContent = "过长的游戏标题用于验证固定位置".repeat(30);});
  const after = await poster.locator("small, :scope > span").evaluateAll((elements) => elements.map((element) => element.getBoundingClientRect().y));
  expect(after).toEqual(before);
  await expect(poster.locator("strong")).toHaveCSS("-webkit-line-clamp", "3");
  await page.mouse.move(0, 0);
  const sizes = testInfo.project.name === "chrome-1280" ? [[1280, 800], [1920, 950]] : [[2560, 1360], [2560, 1440]];
  for (const [width, height] of sizes) {
    await page.setViewportSize({ width: width!, height: height! });
    const rows = await cards.evaluateAll((elements) => elements.map((element) => {
      const card = element.getBoundingClientRect();
      const cover = element.querySelector(".library-game-cover")!.getBoundingClientRect();
      const footer = element.querySelector(".library-game-played")!.getBoundingClientRect();
      return { height: card.height, footerGap: card.bottom - footer.bottom, coverRatio: cover.width / cover.height };
    }));
    for (const row of rows) {
      expect(Math.abs(row.height - rows[0]!.height)).toBeLessThanOrEqual(1);
      expect(Math.abs(row.footerGap - rows[0]!.footerGap)).toBeLessThanOrEqual(1);
      expect(row.coverRatio).toBeCloseTo(3 / 4, 2);
    }
  }
});
