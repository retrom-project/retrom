import { expect, type Page } from "@playwright/test";

export async function expectCardRadii(page: Page) {
  const tokens = await page.evaluate(() => {
    const style = getComputedStyle(document.documentElement);
    return ["cover", "control", "panel", "dialog"].map((role) => style.getPropertyValue(`--radius-${role}`).trim());
  });
  expect(tokens).toEqual(["4px", "6px", "8px", "12px"]);
  if (page.viewportSize()!.width >= 1280) {
    for (const card of await page.locator(".library-game-card, .favorite-game-card").all()) {
      const box = (await card.boundingBox())!;
      expect(box.width).toBeGreaterThanOrEqual(270);
      expect(box.width).toBeLessThanOrEqual(280);
      const cover = (await card.locator(".library-game-cover, .favorite-game-cover").boundingBox())!;
      expect(cover.width / cover.height).toBeCloseTo(3 / 4, 2);
    }
  }
  if (page.viewportSize()!.width >= 1800) {
    for (const card of await page.locator(".home-recent-card").all()) {
      const box = (await card.boundingBox())!;
      expect(box.width / box.height).toBeCloseTo(2, 2);
    }
  }
  const cards = await page.locator(".library-game-card, .favorite-game-card, .save-library-card, .home-recent-card, .home-featured-cover, .home-favorite-cover, .home-featured-save-preview, .game-detail-poster").evaluateAll((elements) => elements
    .filter((element) => element.checkVisibility())
    .map((element) => ({ name: element.className, radius: getComputedStyle(element).borderRadius })));
  for (const card of cards) { expect(card.radius, card.name).toBe("4px"); }
  for (const panel of await page.locator(".home-featured-panel, .home-quick-panel").all()) {
    await expect(panel).toHaveCSS("border-radius", "8px");
  }
  for (const cover of await page.locator(".library-game-cover").all()) {
    const standalone = await cover.evaluate((element) => Boolean(element.closest(".phone-app-frame")));
    await expect(cover).toHaveCSS("border-radius", standalone ? "4px" : "3px 3px 0px 0px");
  }
}
