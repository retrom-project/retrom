import { execFileSync } from "node:child_process";
import path from "node:path";
import { expect, type Page, type TestInfo } from "@playwright/test";
import { evidencePath } from "./acceptance-support";
import { expectNaturalHomeFlow } from "./home-layout-support";

export async function expectHomeStates(page: Page, testInfo: TestInfo) {
  const database = process.env.RETROM_E2E_DATABASE;
  if (!database) { return; } // Live PFB review remains read-only.
  for (const state of ["empty", "played", "saved"]) {
    execFileSync("python3", [path.resolve("../scripts/acceptance/seed-ui-home.py"), database, state]);
    await page.goto("/");
    await expect(page.locator(".home-featured-empty")).toHaveCount(state === "empty" ? 1 : 0);
    await expect(page.locator(".home-featured-save-preview")).toHaveCount(state === "saved" ? 1 : 0);
    if (state === "saved") {
      await expect(page.locator(".home-featured-save-preview img")).toBeVisible();
      await expect.poll(() => page.locator(".home-featured-save-preview img").evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)).toBe(true);
    }
    await expectNaturalHomeFlow(page);
    await page.screenshot({ path: evidencePath(testInfo, `ui-home-${state}.png`), fullPage: true });
  }
}
