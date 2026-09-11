import { expect, test } from "@playwright/test";

test("ACC-UI-005 library cards align with mixed tag counts", async ({ page }, testInfo) => {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const login = await page.request.post("/api/v1/auth/login", { headers: { Origin: origin }, data: { username: "test", password: "test" } });
  expect(login.ok()).toBe(true);
  await page.goto("/library");
  const cards = page.locator(".library-game-card");
  await expect(cards.nth(1)).toBeVisible();
  // Layout-only variation of rendered fixture metadata; never writes user data.
  await cards.first().locator(".library-game-body > .tag-chips").evaluateAll((tags) => tags.forEach((tag) => tag.remove()));
  await cards.nth(1).locator(".library-game-body").evaluate((body) => {
    body.querySelector(".tag-chips")?.remove();
    const tags = document.createElement("div");
    tags.className = "tag-chips";
    tags.textContent = "掌机精选 · 长标签布局回归";
    body.querySelector(".library-game-played")!.before(tags);
  });
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
