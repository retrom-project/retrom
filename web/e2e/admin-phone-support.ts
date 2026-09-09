import { expect, type Page } from "@playwright/test";

export async function expectPhoneAdminNotice(page: Page) {
  await expect(page.getByRole("heading", { name: "请在电脑上管理游戏库" })).toBeVisible();
  await expect(page.getByRole("link", { name: "返回游戏库", exact: true })).toHaveAttribute("href", "/library");
  await expect(page.locator("main form")).toHaveCount(0);
  await expect(page.getByRole("dialog")).toHaveCount(0);
}
