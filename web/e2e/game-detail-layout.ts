import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";

export async function verifyGameDetailLayout(page: Page) {
  const main = page.locator(".game-detail-main");
  await expect(main.locator("h1")).toBeVisible();
  const launch = main.locator(".launch-panel");
  if (!(await launch.count())) {
    await expect(page.locator(".phone-detail-launch")).toBeVisible();
    return;
  }
  await expect(launch.locator(".launch-runtime-choice strong")).not.toHaveText("—");
  const geometry = await main.evaluate(element => {
    const bounds = (selector: string) => element.querySelector(selector)!.getBoundingClientRect();
    const hero = element.closest(".game-detail-hero")!;
    const heroBox = hero.getBoundingClientRect();
    const style = getComputedStyle(hero);
    const box = element.getBoundingClientRect();
    const loading = bounds(".launch-options"), actions = bounds(".launch-actions");
    const hint = bounds(".launch-panel > .launch-hint"), runtime = bounds(".launch-runtime-row");
    const eyebrow = bounds(".game-detail-eyebrow"), title = bounds(".game-detail-title-row");
    const tags = bounds(".game-detail-tags");
    return {
      topGap: box.top - heroBox.top - parseFloat(style.paddingTop) - parseFloat(style.borderTopWidth),
      headingGap: title.top - eyebrow.bottom,
      tagsGap: tags.top - title.bottom,
      loadingGap: loading.top - tags.bottom,
      actionsGap: actions.top - loading.bottom,
      hintGap: hint.top - actions.bottom,
      runtimeGap: runtime.top - hint.bottom,
      bottomGap: box.bottom - runtime.bottom,
      fits: element.scrollHeight <= element.clientHeight + 1,
    };
  });
  expect(Math.abs(geometry.topGap)).toBeLessThanOrEqual(1);
  expect(geometry.headingGap).toBeGreaterThanOrEqual(12);
  expect(geometry.tagsGap).toBeGreaterThanOrEqual(8);
  expect(geometry.loadingGap).toBeGreaterThanOrEqual(12);
  expect(geometry.actionsGap).toBeGreaterThanOrEqual(12);
  expect(geometry.hintGap).toBeGreaterThanOrEqual(8);
  expect(geometry.runtimeGap).toBeGreaterThanOrEqual(12);
  expect(Math.abs(geometry.bottomGap)).toBeLessThanOrEqual(1);
  expect(geometry.fits).toBe(true);
}
