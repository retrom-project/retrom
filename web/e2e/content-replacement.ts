import { expect } from "@playwright/test";
import type { Page, TestInfo } from "@playwright/test";
import type { Schema } from "../lib/api/types";
import { request } from "./clean-refactor-support";

export async function verifyContentReplacement(page: Page, info: TestInfo, game: Schema<"Game">) {
  const base = `/api/v1/admin/games/${game.id}/content-replacement`;
  const before = await request<Schema<"GameDetail">>(page, `/api/v1/admin/games/${game.id}`);
  const source = process.env.RETROM_BROWSER_SOURCE_PATH ?? "/pfb-workspace/acceptance/sources/browser";
  await page.goto(`/admin/games/${game.id}`);
  const section = page.locator("section").filter({ has: page.getByRole("heading", { name: "文件与管理操作", exact: true }) });
  const open = section.getByRole("button", { name: "替换游戏内容", exact: true });
  await expect(open).toBeEnabled();
  await open.click();
  const dialog = page.getByRole("alertdialog");
  await expect(dialog).toContainText("可以关闭页面，稍后在此查看进度");
  await dialog.getByRole("textbox", { name: "服务器内容路径", exact: true }).fill(`${source}/browser-acceptance.nes`);
  const submitted = page.waitForResponse((response) => response.url().endsWith(base) && response.request().method() === "POST");
  await dialog.getByRole("button", { name: "确认", exact: true }).click();
  const response = await submitted;
  expect(response.status()).toBe(202);
  const accepted = await response.json() as Schema<"ContentReplacement">;
  expect(accepted.gameId).toBe(game.id);
  expect(accepted.id).toMatch(/^[0-9a-f-]{36}$/u);
  await expect(dialog).toHaveCount(0);
  await expect(section.getByRole("status")).toBeVisible();
  await section.scrollIntoViewIfNeeded();
  await page.screenshot({ path: info.outputPath("content-replacement-accepted.png") });
  // Reload reads current operation server-side; success is a completed result,
  // never the submission's 202 or a response shaped as GameDetail.
  await page.reload();
  let completed: Schema<"ContentReplacement"> | undefined;
  await expect.poll(async () => {
    completed = await request<Schema<"ContentReplacement">>(page, `${base}/${accepted.id}`);
    expect(completed.error).toBeNull();
    return completed.status;
  }, { timeout: 60_000 }).toBe("completed");
  expect(completed?.result?.version).toBe(before.game.version + 1);
  expect(completed?.result?.contentHash).toBe(before.game.contentHash);
  await expect(section.getByRole("status")).toContainText("游戏内容已替换");
  await expect(open).toBeEnabled();
  await section.scrollIntoViewIfNeeded();
  await page.screenshot({ path: info.outputPath("content-replacement-completed.png") });
  const after = await request<Schema<"GameDetail">>(page, `/api/v1/admin/games/${game.id}`);
  expect(after.game.version).toBe(before.game.version + 1);
  expect(after.game.contentHash).toBe(before.game.contentHash);
  expect(after.files[0]?.id).not.toBe(before.files[0]?.id);
  await info.attach("content-replacement-operation", { body: JSON.stringify({ accepted, completed }), contentType: "application/json" });
}
