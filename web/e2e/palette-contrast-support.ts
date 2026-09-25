import { expect, type Page } from "@playwright/test";
import axe from "axe-core";

async function expectVisibleImagesReady(page: Page) {
  await page.locator("main img").evaluateAll(async (elements) => {
    const visible = elements.filter((element): element is HTMLImageElement => {
      const box = element.getBoundingClientRect();
      return element instanceof HTMLImageElement && element.checkVisibility()
        && box.bottom > 0 && box.top < innerHeight && box.right > 0 && box.left < innerWidth;
    });
    await Promise.all(visible.map((image) => image.decode()));
  });
}

/** Check rendered text, including inherited table cells, against its actual surface. */
export async function expectPaletteContrast(page: Page) {
  await expectVisibleImagesReady(page);
  await expect(page.locator("html")).toHaveCSS("color-scheme", "dark");
  await page.evaluate(axe.source);
  const violations = await page.evaluate(async () => {
    const result = await window.axe.run(document, {
      runOnly: { type: "rule", values: ["color-contrast"] },
    });
    return result.violations;
  });
  expect(violations, `text contrast on ${new URL(page.url()).pathname}`).toEqual([]);
}

/** Opaque statistic surfaces keep text contrast independent of the hero gradient. */
export async function expectServerImportStatsContrast(page: Page) {
  const groups = page.locator(".server-import-capability-stats");
  await expect(groups).toHaveCount(2);
  for (const group of await groups.all()) {
    await expect(group).toBeVisible();
    for (const surface of await group.locator(":scope > div").all()) {
      await expect(surface).toHaveCSS("background-color", /^rgb\(/);
    }
  }
  await expectPaletteContrast(page);
}
