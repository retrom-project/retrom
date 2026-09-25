import { expect, type Page } from "@playwright/test";
import axe from "axe-core";

/** Check rendered text, including inherited table cells, against its actual surface. */
export async function expectPaletteContrast(page: Page) {
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
