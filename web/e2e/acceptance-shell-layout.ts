import { expect, type Page } from "@playwright/test";

export async function expectSidebarFooterAlignment(page: Page) {
  const footer = page.locator(".sidebar-foot");
  const account = await footer.locator(".sidebar-account-row").boundingBox();
  const context = await footer.locator(".context-switch").boundingBox();
  const health = footer.locator(".connection");
  const status = await health.boundingBox();
  expect(account).not.toBeNull();
  expect(context).not.toBeNull();
  expect(status).not.toBeNull();
  if (!account || !context || !status) {throw new Error("Sidebar footer bounds unavailable");}
  expect(account.height).toBe(54);
  expect(context.height).toBe(54);
  expect(context.x).toBe(account.x);
  expect(context.width).toBe(account.width);
  expect(context.y - account.y - account.height).toBe(10);
  expect(status.x).toBeGreaterThanOrEqual(account.x);
  expect(status.x + status.width).toBeLessThanOrEqual(account.x + account.width);
  await footer.locator(".account-menu summary").focus();
  await page.keyboard.press("Tab");
  await expect(health).toBeFocused();
  await expect(health.locator(".connection-tooltip")).toBeVisible();
  const menu = footer.locator(".account-menu");
  await expect(menu).not.toHaveAttribute("open");
  await menu.locator("summary").click();
  await expect(menu).toHaveAttribute("open", "");
  await page.locator(".brand").click();
  await expect(menu).not.toHaveAttribute("open");
}
