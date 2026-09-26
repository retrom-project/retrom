import { expect, test } from "@playwright/test";

test("ACC-UI-005 home posters keep long titles on one line and metadata beneath", async ({ page }, testInfo) => {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  expect((await page.request.post("/api/v1/auth/login", { headers: { Origin: origin }, data: { username: "test", password: "test" } })).ok()).toBe(true);
  await page.goto("/");
  await expect(page.locator(".home-featured-details")).toBeVisible();
  await expect(page.locator(".home-page .tag-chips")).toHaveCount(0);
  await page.locator(".home-recent-copy strong").evaluateAll((titles) => titles.forEach((title) => {
    title.textContent = "很长的游戏名称，用于验证海报卡片中的省略显示";
  }));
  const sizes = testInfo.project.name === "chrome-1280" ? [[1280, 800], [1920, 950], [2086, 920]] : [[2560, 1360], [2560, 1440], [3840, 2160]];
  for (const [width, height] of sizes) {
    await page.setViewportSize({ width: width!, height: height! });
    const geometry = await page.locator(".home-recent-card").evaluateAll((cards) => cards.map((card) => {
      const title = card.querySelector("strong")!;
      const style = getComputedStyle(title);
      const info = card.querySelector(".home-recent-copy small")!.getBoundingClientRect();
      const cover = card.querySelector(".home-recent-cover")!.getBoundingClientRect();
      return { whiteSpace: style.whiteSpace, overflow: style.textOverflow, gap: info.top - title.getBoundingClientRect().bottom, ratio: cover.width / cover.height, width: cover.width };
    }));
    expect(geometry.length).toBeGreaterThan(0);
    for (const card of geometry) {
      expect(card.whiteSpace).toBe("nowrap");
      expect(card.overflow).toBe("ellipsis");
      expect(card.gap).toBeGreaterThanOrEqual(4);
      expect(card.ratio).toBeCloseTo(5 / 7, 2);
      expect(card.width).toBeGreaterThanOrEqual(148);
      expect(card.width).toBeLessThanOrEqual(240);
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  }
});
