import { expect, type Page, type TestInfo } from "@playwright/test";

export async function verifyScanControls(page: Page, info: TestInfo) {
  const local = page.getByRole("button", { name: "本地扫描", exact: true });
  await expect(local).toHaveCount(1);
  await local.click();
  const sheet = page.getByRole("dialog", { name: "选择游戏所在目录", exact: true });
  const format = sheet.getByLabel("文件组织形式", { exact: true });
  const read = sheet.getByRole("button", { name: "读取来源集合", exact: true });
  await expect(format).toHaveValue("");
  await expect(read).toBeDisabled();
  const pathBox = await sheet.getByLabel("服务器目录", { exact: true }).boundingBox();
  const formatBox = await format.boundingBox();
  expect(formatBox!.y).toBeGreaterThan(pathBox!.y + pathBox!.height);
  await format.focus();
  await page.screenshot({ path: info.outputPath("local-scan-unselected.png") });
  for (const value of ["pegasus", "emulationstation"]) {
    await format.selectOption(value);
    await expect(read).toBeEnabled();
    await expect(sheet).toContainText(value === "pegasus" ? "metadata.pegasus.txt" : "gamelist.xml");
  }
  await page.screenshot({ path: info.outputPath("local-scan-selected.png") });
  await sheet.getByRole("button", { name: "取消", exact: true }).click();
  await local.click();
  await expect(format).toHaveValue("");
  await expect(read).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(sheet).toHaveCount(0);

  await page.getByRole("button", { name: "选择 BIOS 目录", exact: true }).click();
  const bios = page.getByRole("dialog", { name: "BIOS扫描", exact: true });
  const platforms = bios.getByRole("group", { name: "平台范围", exact: true });
  const cores = bios.getByRole("group", { name: "核心范围", exact: true });
  await expect(platforms.locator(".workspace-tags").getByRole("checkbox").first()).toBeVisible();
  await platforms.getByRole("checkbox", { name: "全选平台范围", exact: true }).click();
  await expect.poll(() => platforms.locator(".workspace-tags").getByRole("checkbox").evaluateAll((items) => items.every((item) => (item as HTMLInputElement).checked))).toBe(true);
  await expect.poll(() => cores.locator(".workspace-tags").getByRole("checkbox").evaluateAll((items) => items.length > 0 && items.every((item) => !(item as HTMLInputElement).checked))).toBe(true);
  await cores.getByRole("checkbox", { name: "全选核心范围", exact: true }).click();
  await expect.poll(() => cores.locator(".workspace-tags").getByRole("checkbox").evaluateAll((items) => items.every((item) => (item as HTMLInputElement).checked))).toBe(true);
  await page.screenshot({ path: info.outputPath("bios-scan-all-selected.png") });
  await platforms.getByRole("checkbox", { name: "取消全选平台范围", exact: true }).click();
  await expect.poll(() => platforms.locator(".workspace-tags").getByRole("checkbox").evaluateAll((items) => items.every((item) => !(item as HTMLInputElement).checked))).toBe(true);
  await expect(cores.getByRole("checkbox", { name: "取消全选核心范围", exact: true })).toBeVisible();
  await cores.locator(".workspace-tags").getByRole("checkbox").first().uncheck();
  await expect(cores.getByRole("checkbox", { name: "全选核心范围", exact: true })).toHaveJSProperty("indeterminate", true);
  await expect(cores.getByRole("checkbox", { name: "全选核心范围", exact: true })).toBeVisible();
  await cores.getByRole("checkbox", { name: "全选核心范围", exact: true }).click();
  await cores.getByRole("checkbox", { name: "取消全选核心范围", exact: true }).click();
  await expect(bios.getByRole("button", { name: "开始扫描", exact: true })).toBeDisabled();
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: info.outputPath("bios-scan-unselected.png") });
  await page.keyboard.press("Escape");
  await expect(bios).toHaveCount(0);
}
