import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { expect, test, type TestInfo } from "@playwright/test";
import { prepareNewSourceScan, selectServerSource } from "./server-directory-support";
import { expectSourceHandoffDuringCleanup } from "./server-import-cleanup-support";

function screenshotPath(info: TestInfo, name: string) {
  const directory = process.env.RETROM_ACCEPTANCE_CASE_DIR;
  if (!directory) {return info.outputPath(name);}
  const screenshots = path.join(directory, "screenshots");
  mkdirSync(screenshots, { recursive: true });
  return path.join(screenshots, `${info.project.name}-${name}`);
}

test("ACC-BASIC-001 directory extensions feed the shared receive and review flow", async ({ page }, info) => {
  test.setTimeout(120_000);
  const source = process.env.RETROM_E2E_SERVER_SOURCE;
  expect(source).toBeTruthy();
  // Each viewport must prepare a new review rather than replay the game
  // published by a preceding viewport in the shared acceptance catalog.
  const fixture = readFileSync(new URL("../../testdata/public-roms/gba-smoke/pegasus-smoke.gba", import.meta.url));
  writeFileSync(path.join(source!, "Basic/nested/basic-smoke.GBA"),
    Buffer.concat([fixture, Buffer.from(`retrom-basic-e2e:${info.project.name}`)]));
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const login = await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" }, headers: { Origin: origin },
  });
  expect(login.ok()).toBe(true);
  await page.goto("/admin/imports/server?action=source");
  const drawer = page.getByRole("dialog", { name: "从目录准备审核事项" });
  await prepareNewSourceScan(drawer);
  const format = drawer.getByRole("combobox", { name: "文件组织格式" });
  const extensions = drawer.getByRole("textbox", { name: "扩展名筛选" });
  await expect(format).toHaveValue("");
  await expect(drawer.getByRole("button", { name: "扫描此目录" })).toBeDisabled();
  await expect(drawer.getByRole("radio")).toHaveCount(0);
  for (const value of ["PEGASUS", "GAMELIST"]) {
    await format.selectOption(value);
    await expect(extensions).toBeDisabled();
  }
  await format.selectOption("BASIC");
  await expect(extensions).toBeEnabled();
  await extensions.fill(".gba");
  await selectServerSource(drawer, "Basic");
  await page.screenshot({ path: screenshotPath(info, "basic-selection.png"), fullPage: false });
  const created = page.waitForResponse((response) => new URL(response.url()).pathname === "/api/v1/admin/source-imports" && response.request().method() === "POST");
  await drawer.getByRole("button", { name: "扫描此目录" }).click();
  const plan = await (await created).json() as { id: string; format: string; extensionFilter: string };
  expect(plan).toMatchObject({ format: "BASIC", extensionFilter: ".gba" });
  const mapping = drawer.getByRole("button", { name: "所选目录 处理方式" });
  await expect(mapping).toBeVisible({ timeout: 30_000 });
  await expect(mapping).toContainText("请选择，不会自动映射");
  await expect(drawer.getByRole("button", { name: "确认映射" })).toBeDisabled();
  await mapping.click();
  const choices = drawer.getByRole("region", { name: "可选游戏目录" });
  await choices.getByRole("searchbox", { name: "搜索目录、平台或核心" }).fill("mGBA");
  await choices.getByRole("button", { name: /GBA 游戏/ }).click();
  await page.screenshot({ path: screenshotPath(info, "basic-mapping.png"), fullPage: false });
  await drawer.getByRole("button", { name: "确认映射" }).click();
  await expect(drawer).toContainText("1 / 0 个游戏");
  await drawer.getByRole("button", { name: "开始准备审核事项" }).click();
  await expect(page).toHaveURL(new RegExp(`/admin/imports/server/source/${plan.id}$`));
  await expect(page.getByText("审核事项已生成", { exact: true })).toBeVisible({ timeout: 60_000 });
  const results = await page.request.get(`/api/v1/admin/source-imports/${plan.id}/items?limit=20`);
  const payload = await results.json() as { items: Array<{ title: string; executionState: string; reviewItemId: string; publishedGameId: string | null }> };
  expect(payload.items).toHaveLength(1);
  const item = payload.items[0];
  expect(item).toMatchObject({ title: "basic-smoke", executionState: "REVIEW_PENDING", publishedGameId: null });
  await expect(page.getByText("已移交审核", { exact: true })).toBeVisible({ timeout: 20_000 });
  await expectSourceHandoffDuringCleanup(page, plan.id);
  await page.goto(`/admin/reviews/${item.reviewItemId}`);
  const approve = page.getByRole("button", { name: "通过并发布" });
  await expect(approve).toBeEnabled({ timeout: 30_000 });
  await page.screenshot({ path: screenshotPath(info, "basic-review.png"), fullPage: true });
  await approve.click();
  await expect(page.locator(".app-toast")).toContainText("游戏已成功发布", { timeout: 20_000 });
  await expect.poll(async () => {
    const response = await page.request.get(`/api/v1/admin/source-imports/${plan.id}/items?limit=20`);
    const data = await response.json() as { items: Array<{ executionState: string }> };
    return data.items[0].executionState;
  }, { timeout: 20_000 }).toBe("PUBLISHED");
});
