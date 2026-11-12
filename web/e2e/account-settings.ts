import { expect, type Page, type TestInfo } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { Schema } from "../lib/api/types";
import { request } from "./clean-refactor-support";

export async function verifyAccountSettings(page: Page, testInfo: TestInfo) {
  const profile = page.locator(".account-settings-grid > .panel").first();
  const application = page.getByRole("region", { name: "Retrom 应用" });
  const trigger = profile.getByRole("button", { name: "修改密码", exact: true });
  await expect(trigger).toBeVisible();
  await expect(page.getByLabel("当前密码", { exact: true })).toHaveCount(0);
  const heading = profile.locator(".panel-head");
  const [profileBox, applicationBox, headingBox, triggerBox] = await Promise.all(
    [profile, application, heading, trigger].map(element => element.boundingBox()),
  );
  expect(profileBox).not.toBeNull();
  expect(applicationBox).not.toBeNull();
  expect(headingBox).not.toBeNull();
  expect(triggerBox).not.toBeNull();
  expect(triggerBox!.x + triggerBox!.width).toBeLessThanOrEqual(headingBox!.x + headingBox!.width);
  expect(triggerBox!.y).toBeGreaterThan(headingBox!.y);
  if ((page.viewportSize()?.width ?? 0) > 900) {
    expect(Math.abs(profileBox!.y - applicationBox!.y)).toBeLessThan(1);
    expect(Math.abs(profileBox!.height - applicationBox!.height)).toBeLessThan(1);
    expect(applicationBox!.x).toBeGreaterThan(profileBox!.x + profileBox!.width);
  } else {
    expect(applicationBox!.y).toBeGreaterThan(profileBox!.y + profileBox!.height);
  }
  const updateButton = application.getByRole("button", { name: "检查更新", exact: true });
  const actionInsets = await updateButton.evaluate(button => {
    const panel = button.closest(".panel")!;
    const body = button.closest(".panel-body")!;
    const panelBox = panel.getBoundingClientRect();
    const buttonBox = button.getBoundingClientRect();
    const panelStyle = getComputedStyle(panel);
    const bodyStyle = getComputedStyle(body);
    return {
      bottom: panelBox.bottom - buttonBox.bottom,
      right: panelBox.right - buttonBox.right,
      expectedBottom: parseFloat(panelStyle.borderBottomWidth) + parseFloat(bodyStyle.paddingBottom),
      expectedRight: parseFloat(panelStyle.borderRightWidth) + parseFloat(bodyStyle.paddingRight),
    };
  });
  expect(Math.abs(actionInsets.bottom - actionInsets.expectedBottom)).toBeLessThan(1);
  expect(Math.abs(actionInsets.right - actionInsets.expectedRight)).toBeLessThan(1);
  await expect(application.getByText(/离线提示/u)).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath("account-settings.png"), fullPage: true });

  await page.evaluate(async () => { await navigator.serviceWorker.ready; });
  await updateButton.click();
  const notification = page.locator(".app-toast");
  await expect(notification).toHaveText(/当前已经是最新版本了。/u);
  await page.screenshot({ path: testInfo.outputPath("account-latest-version.png") });
  await notification.getByRole("button", { name: "关闭通知" }).click();

  await trigger.click();
  const dialog = page.getByRole("dialog", { name: "修改密码", exact: true });
  const current = dialog.getByLabel("当前密码", { exact: true });
  await current.fill("original-password");
  await dialog.getByLabel("新密码", { exact: true }).fill("replacement-password");
  await dialog.getByLabel("确认密码", { exact: true }).fill("different-password");
  await dialog.getByRole("button", { name: "显示当前密码", exact: true }).click();
  await expect(current).toHaveAttribute("type", "text");
  await dialog.getByRole("button", { name: "隐藏当前密码", exact: true }).click();
  await expect(current).toHaveAttribute("type", "password");
  await dialog.getByRole("button", { name: "更新密码", exact: true }).click();
  await expect(dialog.getByText("两次输入的新密码不一致。")).toBeVisible();
  await current.focus();
  await page.screenshot({ path: testInfo.outputPath("account-password-dialog.png") });
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await trigger.click();
  await expect(current).toHaveValue("");
  await expect(dialog.getByLabel("新密码", { exact: true })).toHaveValue("");
  await expect(dialog.getByText("两次输入的新密码不一致。")).toHaveCount(0);
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
}

export async function verifyPasswordSession(administrator: Page) {
  const invitation = await request<Schema<"AccountLink">>(administrator, "/api/v1/admin/invitations", {
    role: "user", expiresInHours: 1,
  });
  const token = new URLSearchParams(new URL(invitation.url).hash.slice(1)).get("token");
  const browser = administrator.context().browser();
  if (!browser || !token) { throw new Error("Password session verification requires a browser and invitation"); }
  const origin = new URL(administrator.url()).origin;
  const context = await browser.newContext({ baseURL: origin });
  try {
    const original = "Browser-original-" + randomUUID();
    const replacement = "Browser-replacement-" + randomUUID();
    const accepted = await context.request.post("/api/v1/auth/invitations/accept", {
      headers: { Origin: origin },
      data: { token, username: "password-" + randomUUID().slice(0, 8), displayName: "Password review", password: original },
    });
    expect(accepted.ok()).toBe(true);
    const page = await context.newPage();
    await page.goto("/account");
    for (const [current, next] of [[original, replacement], [replacement, original]]) {
      await page.getByRole("button", { name: "修改密码", exact: true }).click();
      const dialog = page.getByRole("dialog", { name: "修改密码", exact: true });
      await dialog.getByLabel("当前密码", { exact: true }).fill(current);
      await dialog.getByLabel("新密码", { exact: true }).fill(next);
      await dialog.getByLabel("确认密码", { exact: true }).fill(next);
      const response = page.waitForResponse(item => item.url().endsWith("/api/v1/auth/change-password") && item.request().method() === "POST");
      await dialog.getByLabel("确认密码", { exact: true }).press("Enter");
      expect((await response).ok()).toBe(true);
      await expect(dialog).toHaveCount(0);
      await expect(page.locator(".app-toast")).toContainText("密码已更新，其他设备已退出登录。");
      await page.locator(".app-toast").getByRole("button", { name: "关闭通知" }).click();
    }
  } finally { await context.close(); }
}
