import { expect, type Page } from "@playwright/test";

/** Measure visible glyphs, not just the flex-aligned line box. */
export async function expectStatusTextCentered(page: Page) {
  await page.evaluate(() => document.fonts.ready);
  const labels = await page.locator(".admin-game-table .status").evaluateAll((elements) => elements.map((element) => {
    const style = getComputedStyle(element);
    const box = element.getBoundingClientRect();
    const range = document.createRange();
    range.selectNodeContents(element);
    const text = range.getBoundingClientRect();
    const context = document.createElement("canvas").getContext("2d")!;
    context.font = style.font;
    const metrics = context.measureText(element.textContent ?? "");
    const baseline = text.top + metrics.fontBoundingBoxAscent;
    const glyphCenter = baseline + (metrics.actualBoundingBoxDescent - metrics.actualBoundingBoxAscent) / 2;
    return {
      label: element.textContent,
      offset: glyphCenter - (box.top + box.height / 2),
      height: box.height,
      verticalPadding: parseFloat(style.paddingTop) + parseFloat(style.paddingBottom),
    };
  }));
  expect(labels.length).toBeGreaterThan(0);
  for (const label of labels) {
    expect(Math.abs(label.offset), `${label.label}: glyph center offset`).toBeLessThanOrEqual(0.5);
    expect(label.height).toBe(25);
    expect(label.verticalPadding).toBe(4);
  }
}
