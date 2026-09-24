import { expect, type Page } from "@playwright/test";

export async function expectNaturalHomeFlow(page: Page) {
  const original = page.viewportSize()!;
  await expect(page.getByText("正在读取收藏…", { exact: true })).toBeHidden();
  const heights: number[] = [];
  for (const height of [900, 1440, 2160]) {
    await page.setViewportSize({ width: original.width, height });
    const layout = await page.locator('.home-page').evaluate((home) => {
      const sections = [...home.querySelectorAll(':scope > .home-layer')].map((element) => element.getBoundingClientRect());
      return { height: home.getBoundingClientRect().height, gaps: sections.slice(1).map((box, index) => box.top - sections[index]!.bottom), overflow: document.documentElement.scrollWidth - innerWidth };
    });
    const cards = await page.locator(".home-first-layer > .panel").evaluateAll((elements) => elements.map((element) => { const box = element.getBoundingClientRect(); return { top: box.top, bottom: box.bottom }; }));
    expect(cards).toHaveLength(2);
    expect(Math.abs(cards[0]!.top - cards[1]!.top)).toBeLessThanOrEqual(1);
    expect(Math.abs(cards[0]!.bottom - cards[1]!.bottom)).toBeLessThanOrEqual(1);
    if (original.width >= 1800 && await page.locator(".home-featured-cover").count()) {
      const alignment = await page.locator(".home-featured-cover").evaluate((cover) => {
        const box = cover.getBoundingClientRect(), media = cover.parentElement!, mediaBox = media.getBoundingClientRect();
        const actions = media.querySelector(".home-featured-actions")!.getBoundingClientRect(), style = getComputedStyle(media);
        return { bottom: box.bottom - actions.bottom, height: box.height - mediaBox.height + parseFloat(style.paddingTop) + parseFloat(style.paddingBottom), ratio: box.width / box.height };
      });
      expect(Math.abs(alignment.bottom)).toBeLessThanOrEqual(1);
      expect(Math.abs(alignment.height)).toBeLessThanOrEqual(1);
      expect(alignment.ratio).toBeCloseTo(5 / 7, 2);
    }
    if (original.width >= 1800 && await page.locator(".home-featured-save-preview").count()) {
      const copy = (await page.locator(".home-featured-copy").boundingBox())!;
      const preview = (await page.locator(".home-featured-save-preview").boundingBox())!;
      const media = (await page.locator(".home-featured-media").boundingBox())!;
      expect(preview.x).toBeGreaterThanOrEqual(copy.x + copy.width + 23);
      expect(preview.x + preview.width).toBeLessThanOrEqual(media.x + media.width);
      expect(preview.width / preview.height).toBeCloseTo(16 / 9, 2);
      expect(preview.width).toBeLessThanOrEqual(560);
      expect(preview.width / media.width).toBeGreaterThanOrEqual(.24);
    }
    heights.push(layout.height);
    for (const gap of layout.gaps) { expect(gap).toBeGreaterThanOrEqual(24); }
    expect(layout.overflow).toBeLessThanOrEqual(0);
  }
  expect(Math.max(...heights) - Math.min(...heights)).toBeLessThanOrEqual(1);
  await page.setViewportSize(original);
}
