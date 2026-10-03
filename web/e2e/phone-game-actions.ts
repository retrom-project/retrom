import {expect, type Locator, type Page, type TestInfo} from "@playwright/test";
import {evidencePath} from "./acceptance-support";

export async function expectPhoneGameActions(page: Page, card: Locator, testInfo: TestInfo) {
  const trigger = card.getByRole("button", {name: /的更多操作/});
  await trigger.tap();
  const sheet = page.getByRole("dialog", {name: /的更多操作/});
  await expect(sheet).toBeVisible();
  expect(await sheet.evaluate(element => element.closest(".library-game-card") === null)).toBe(true);
  // Sample every visible heart: both the sheet and its backdrop must win hit testing.
  const covered = await page.locator(".favorite-heart").evaluateAll(elements => elements.map(element => {
    const rect = element.getBoundingClientRect();
    const x = rect.x + rect.width / 2, y = rect.y + rect.height / 2;
    return y < 0 || y >= innerHeight || Boolean(document.elementFromPoint(x, y)?.closest(".responsive-sheet-layer"));
  }));
  expect(covered.every(Boolean)).toBe(true);
  await page.screenshot({path: evidencePath(testInfo, "phone-game-actions.png")});
  await sheet.getByRole("button", {name: "管理收藏夹"}).tap();
  const picker = page.getByRole("dialog", {name: /收藏夹/});
  await expect(picker).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(picker).toHaveCount(0);
  await expect(trigger).toBeFocused();
}
