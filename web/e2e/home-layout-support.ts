import { expect, type Page } from "@playwright/test";

export async function expectHomeHero(page: Page) {
  const hero = page.locator(".home-featured-panel");
  await expect(hero).toHaveCount(1);
  await expect(hero).toHaveCSS("border-radius", "8px");
  await expect(hero.locator(".home-featured-cover, .home-featured-save-preview")).toHaveCount(0);
  const geometry = await hero.evaluate((element) => {
    const panel = element.getBoundingClientRect();
    const content = [...element.querySelectorAll("h2, .home-featured-actions, .home-save-link")].map((item) => item.getBoundingClientRect());
    return { width: panel.width, parentWidth: element.parentElement!.getBoundingClientRect().width,
      contained: content.every((item) => item.left >= panel.left && item.right <= panel.right && item.top >= panel.top && item.bottom <= panel.bottom) };
  });
  expect(geometry.contained).toBe(true);
  expect(Math.abs(geometry.width - geometry.parentWidth)).toBeLessThanOrEqual(1);
}

export async function expectNaturalHomeFlow(page: Page) {
  const original = page.viewportSize()!;
  await expect(page.getByRole("heading", { name: "最新添加", exact: true })).toHaveCount(0);
  await expect(page.getByText("正在读取收藏…", { exact: true })).toBeHidden();
  const heights: number[] = [];
  for (const height of [900, 1440, 2160]) {
    await page.setViewportSize({ width: original.width, height });
    await expectHomeHero(page);
    const layout = await page.locator('.home-page').evaluate((home) => {
      const sections = [...home.querySelectorAll(':scope > .home-layer')].map((element) => element.getBoundingClientRect());
      return { height: home.getBoundingClientRect().height, gaps: sections.slice(1).map((box, index) => box.top - sections[index]!.bottom), overflow: document.documentElement.scrollWidth - innerWidth };
    });
    heights.push(layout.height);
    for (const gap of layout.gaps) { expect(gap).toBeGreaterThanOrEqual(24); }
    expect(layout.overflow).toBeLessThanOrEqual(0);
  }
  expect(Math.max(...heights) - Math.min(...heights)).toBeLessThanOrEqual(1);
  await page.setViewportSize(original);
}
