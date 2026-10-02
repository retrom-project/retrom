import { expect, type Page } from "@playwright/test";

export async function expectLaunchFieldWidth(page: Page) {
  const actions = page.locator(".launch-actions");
  await expect(actions).toBeVisible();
  const first = (await actions.locator("button").first().boundingBox())!;
  const last = (await actions.locator("button").last().boundingBox())!;
  for (const field of await page.locator(".launch-panel .content-loading-field select:visible").all()) {
    const box = (await field.boundingBox())!;
    expect(Math.abs(box.x - first.x)).toBeLessThan(1);
    expect(Math.abs(box.x + box.width - last.x - last.width)).toBeLessThan(1);
  }
}

export async function expectHomePlatformTextCentered(page: Page) {
  await page.evaluate(() => document.fonts.ready);
  const badges = await page.locator(".home-poster-platform, .home-featured-platform").evaluateAll(elements => elements.map(element => {
    const text = element.firstElementChild ?? element;
    const range = document.createRange();
    range.selectNodeContents(text);
    const box = element.getBoundingClientRect();
    const context = document.createElement("canvas").getContext("2d")!;
    context.font = getComputedStyle(text).font;
    const metrics = context.measureText(text.textContent ?? "");
    const baseline = range.getBoundingClientRect().top + metrics.fontBoundingBoxAscent;
    const center = baseline + (metrics.actualBoundingBoxDescent - metrics.actualBoundingBoxAscent) / 2;
    return { label: text.textContent, offset: center - box.top - box.height / 2 };
  }));
  expect(badges.length).toBeGreaterThan(0);
  for (const badge of badges) {
    expect(Math.abs(badge.offset), `${badge.label} glyph center`).toBeLessThanOrEqual(1.25);
  }
}

export async function expectPlatformLabelVariants(page: Page) {
  // Exercise the actual card markup and CSS with labels of different glyph metrics.
  // Keep this isolated typography probe separate from the unmodified page screenshot.
  const badge = page.locator(".home-poster-platform").first();
  const original = await badge.innerHTML();
  try {
    for (const label of ["WASM-4", "Arcade", "Game Boy Advance", "PlayStation", "中文平台"]) {
      await badge.evaluate((element, value) => { (element.firstElementChild ?? element).textContent = value; }, label);
      await expectHomePlatformTextCentered(page);
    }
  } finally { await badge.evaluate((element, html) => { element.innerHTML = html; }, original); }
}
