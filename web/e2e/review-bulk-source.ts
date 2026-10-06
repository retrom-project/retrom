import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { expect, test } from "@playwright/test";
import { evidencePath } from "./acceptance-support";
import { prepareNewSourceScan, selectServerSource } from "./server-directory-support";

export function registerSourceBulkApprovalTest() {
  test("ACC-UI-010 source review skips duplicate content and refreshes its queue without reloading", async ({ page }, info) => {
    test.setTimeout(90_000);
    const source = process.env.RETROM_E2E_SERVER_SOURCE;
    expect(source, "disposable server source fixture root").toBeTruthy();
    const directory = path.join(source!, "BulkApproval");
    mkdirSync(directory, { recursive: true });
    const fixture = new URL("../../testdata/public-roms/gba-smoke/pegasus-smoke.gba", import.meta.url);
    // Keep the pair identical, but isolate it from other imports and viewports.
    const bytes = Buffer.concat([readFileSync(fixture), Buffer.from(`retrom-bulk-e2e:${info.project.name}`)]);
    writeFileSync(path.join(directory, "First.gba"), bytes);
    writeFileSync(path.join(directory, "Second.gba"), bytes);
    await page.goto("/admin/imports/server?action=source");
    const drawer = page.getByRole("dialog", { name: "从目录准备审核事项" });
    await prepareNewSourceScan(drawer);
    await drawer.getByRole("combobox", { name: "文件组织格式" }).selectOption("BASIC");
    await drawer.getByRole("textbox", { name: "扩展名筛选" }).fill(".gba");
    await selectServerSource(drawer, "BulkApproval");
    const created = page.waitForResponse(response => new URL(response.url()).pathname === "/api/v1/admin/source-imports" && response.request().method() === "POST");
    await drawer.getByRole("button", { name: "扫描此目录" }).click();
    const plan = await (await created).json() as { id: string };
    await drawer.getByRole("button", { name: "所选目录 处理方式" }).click();
    const choices = drawer.getByRole("region", { name: "可选游戏目录" });
    await choices.getByRole("searchbox", { name: "搜索目录、平台或核心" }).fill("mGBA");
    await choices.getByRole("button", { name: /GBA 游戏/ }).click();
    await drawer.getByRole("button", { name: "确认映射" }).click();
    await drawer.getByRole("button", { name: "开始准备审核事项" }).click();
    await expect(page.getByText("审核事项已生成", { exact: true })).toBeVisible({ timeout: 30_000 });
    await page.getByRole("link", { name: /逐项审核 2 个游戏/ }).click();
    await expect(page.getByRole("heading", { name: "审核这批来源游戏" })).toBeVisible();
    await expect(page.locator(".review-workflow-row")).toHaveCount(2);
    const items = await (await page.request.get(`/api/v1/admin/reviews?sourceImportId=${plan.id}&limit=20`)).json() as { items: Array<{ itemId: string }> };
    await page.getByRole("button", { name: "快速审批全部待审" }).click();
    await expect(page.getByRole("heading", { name: "快速审批已完成" })).toBeVisible({ timeout: 15_000 });
    await expect(page.locator(".review-workflow-row")).toHaveCount(0);
    const result = page.locator(".review-bulk-status");
    await expect(result.getByText("已发布", {exact: true}).locator("..")).toHaveText("已发布1");
    await expect(result.getByText("重复已跳过", {exact: true}).locator("..")).toHaveText("重复已跳过1");
    await expect(result.getByText("继续待审", {exact: true}).locator("..")).toHaveText("继续待审0");
    const remaining = await (await page.request.get(`/api/v1/admin/reviews?sourceImportId=${plan.id}&limit=20`)).json() as { items: Array<{ itemId: string }> };
    expect(remaining.items).toHaveLength(0);
    for (const item of items.items) {
      await expect(page.locator(`[data-review-item="${item.itemId}"]`)).toHaveCount(0);
    }
    expect(new URL(page.url()).searchParams.get("sourceImportId")).toBe(plan.id);
    await page.screenshot({ path: evidencePath(info, "source-bulk-approval-updated.png"), fullPage: true });
    await page.reload();
    await expect(page.getByRole("heading", { name: "快速审批已完成" })).toBeVisible();
    await expect(page.locator(".review-workflow-row")).toHaveCount(0);
  });
}
