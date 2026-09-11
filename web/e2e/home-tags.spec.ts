import { expect, test } from "@playwright/test";

test("ACC-UI-005 home tags stay below titles and above the bottom time row", async ({ page }, testInfo) => {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  expect((await page.request.post("/api/v1/auth/login", { headers: { Origin: origin }, data: { username: "test", password: "test" } })).ok()).toBe(true);
  await page.goto("/");
  await expect(page.locator(".home-featured-details")).toBeVisible();
  // Exercise long labels without changing the user's persisted tags.
  await page.locator(".home-featured-details, .home-recent-bottom").evaluateAll((elements) => elements.forEach((element) => {
    element.querySelector(".tag-chips")?.remove();
    const chips = document.createElement("div");
    chips.className = "tag-chips";
    chips.innerHTML = '<span class="tag-chip">很长的游戏分类标签用于验证窄卡片</span><span class="tag-chip">掌机精选</span><span class="tag-chip tag-chip-more">+3</span>';
    if (element.classList.contains("home-featured-details")) {element.querySelector("h2")!.after(chips);}
    else {element.prepend(chips);}
  }));
  const sizes = testInfo.project.name === "chrome-1280" ? [[1280, 800], [1920, 950], [2086, 920]] : [[2560, 1360], [2560, 1440], [3840, 2160]];
  for (const [width, height] of sizes) {
    await page.setViewportSize({ width: width!, height: height! });
    const geometry = await page.evaluate(() => {
      const featured = document.querySelector(".home-featured-details")!;
      const gap = featured.querySelector(".tag-chips")!.getBoundingClientRect().top - featured.querySelector("h2")!.getBoundingClientRect().bottom;
      const cards = [...document.querySelectorAll(".home-recent-card")].map((card) => {
        const tags = card.querySelector(".tag-chips")!.getBoundingClientRect();
        const info = card.querySelector(".home-recent-copy > small")!.getBoundingClientRect();
        const meta = card.querySelector(".home-recent-meta")!.getBoundingClientRect();
        const rect = card.getBoundingClientRect();
        return { infoGap: tags.top - info.bottom, timeGap: meta.top - tags.bottom, bottomGap: rect.bottom - meta.bottom, rightGap: rect.right - tags.right };
      });
      return { gap, cards, overflow: document.documentElement.scrollWidth > innerWidth };
    });
    expect(geometry.gap).toBe(12);
    expect(geometry.cards.length).toBeGreaterThan(0);
    expect(geometry.overflow).toBe(false);
    for (const card of geometry.cards) {
      expect(card.infoGap).toBeGreaterThanOrEqual(4);
      expect(card.timeGap).toBeGreaterThanOrEqual(4);
      expect(card.bottomGap).toBeGreaterThanOrEqual(4);
      expect(card.rightGap).toBeGreaterThanOrEqual(4);
    }
    if (width === 2560) {await page.screenshot({ path: testInfo.outputPath(`home-tags-${height}.png`), scale: "css" });}
  }
});
