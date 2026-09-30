import {expect, type Page} from "@playwright/test";

export async function expectDetailHeroAlignment(page: Page) {
  const main = (await page.locator(".game-detail-main").boundingBox())!;
  for (const selector of [".game-detail-poster", ".game-detail-feature-preview"]) {
    const column = page.locator(selector);
    if (!await column.count()) {continue;}
    const box = (await column.boundingBox())!;
    expect(Math.abs(box.y - main.y), `${selector} top aligns with main`).toBeLessThan(1);
    expect(Math.abs(box.height - main.height), `${selector} height follows main`).toBeLessThan(1);
  }
  const buttons = page.locator(".launch-actions button");
  const last = await buttons.last().boundingBox();
  if (last) {
    for (const field of await page.locator(".content-loading-field select").all()) {
      const box = (await field.boundingBox())!;
      expect(Math.abs(box.x + box.width - last.x - last.width), "loading selector ends with the last launch button").toBeLessThan(1);
    }
  }
}
