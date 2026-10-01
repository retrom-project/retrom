import { mkdirSync } from "node:fs";
import path from "node:path";
import { expect, test, type Page, type TestInfo } from "@playwright/test";

type Directory = { id: string; name: string; createdAtMs: number };

async function screenshot(page: Page, info: TestInfo, state: string) {
  const directory = process.env.RETROM_ACCEPTANCE_CASE_DIR;
  const name = `${info.project.name}-${state}.png`;
  const target = directory ? path.join(directory, "screenshots", name) : info.outputPath(name);
  mkdirSync(path.dirname(target), { recursive: true });
  await page.screenshot({ path: target, fullPage: true });
}

async function createDirectory(page: Page, info: TestInfo, name: string): Promise<Directory> {
  await page.getByRole("button", { name: "新建游戏目录", exact: true }).first().click();
  const drawer = page.getByRole("dialog", { name: "新建游戏目录" });
  await drawer.getByRole("combobox", { name: "游戏平台" }).selectOption("gba");
  await drawer.getByRole("textbox", { name: "目录名称", exact: true }).fill(name);
  await expect(drawer).toContainText("所属分类：掌机");
  await screenshot(page, info, "create");
  const response = page.waitForResponse((result) => result.url().endsWith("/api/v1/admin/platform-instances") && result.request().method() === "POST");
  await drawer.getByRole("button", { name: "创建目录", exact: true }).click();
  const created = await response;
  expect(created.status()).toBe(201);
  const directory = await created.json() as Directory;
  expect(directory.createdAtMs).toBeGreaterThan(0);
  expect(directory).not.toHaveProperty("sortOrder");
  await expect(drawer).toBeHidden();
  await expect(page.locator(`#directory-${directory.id}`)).toBeFocused();
  return directory;
}

test("ACC-PLAT-007 category browsing preserves management and creation order", async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const login = await page.request.post("/api/v1/auth/login", { data: { username: "test", password: "test" }, headers: { Origin: origin } });
  expect(login.ok()).toBe(true);
  await page.goto("/admin/platform-instances");
  await expect(page.getByRole("heading", { name: "游戏目录", exact: true })).toBeVisible();
  await screenshot(page, testInfo, "collapsed");
  const first = await createDirectory(page, testInfo, `Z 较早目录 ${testInfo.project.name}`);
  const second = await createDirectory(page, testInfo, `A 较晚目录 ${testInfo.project.name}`);
  const row = page.locator(`#directory-${second.id}`);
  const names = await page.locator('.platform-directory-group[aria-label="掌机"] .platform-directory-copy h3').allTextContents();
  expect(names.indexOf(first.name)).toBeLessThan(names.indexOf(second.name));
  await page.getByRole("button", { name: "全部收起" }).click();
  const handheld = page.getByRole("button", { name: /^掌机 \d+ 个目录$/ });
  await handheld.focus();
  await page.keyboard.press("Enter");
  await expect(handheld).toHaveAttribute("aria-expanded", "true");
  const search = page.getByRole("searchbox", { name: "搜索目录" });
  await search.fill("找不到的目录");
  await expect(page.getByRole("heading", { name: "没有匹配的游戏目录" })).toBeVisible();
  await screenshot(page, testInfo, "no-results");
  await search.fill(second.name);
  await expect(row).toBeVisible();
  await expect(page.locator(".platform-directory-row")).toHaveCount(1);
  await row.getByRole("button", { name: `管理目录“${second.name}”` }).click();
  const menu = page.getByRole("menu", { name: `管理目录“${second.name}”` });
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("menuitem", { name: "删除空目录" })).toBeInViewport();
  await screenshot(page, testInfo, "single-row-menu");
  await page.keyboard.press("Escape");
  await expect(menu).toBeHidden();
  await expect(row.getByRole("button", { name: `管理目录“${second.name}”` })).toBeFocused();
  await row.getByRole("button", { name: `管理目录“${second.name}”` }).click();
  await page.getByRole("menuitem", { name: "编辑说明" }).click();
  await row.getByRole("textbox", { name: "给用户看的说明" }).fill("分类目录验收说明");
  await row.getByRole("button", { name: "保存", exact: true }).click();
  await expect(row).toContainText("分类目录验收说明");
  await search.clear();
  await expect(handheld).toHaveAttribute("aria-expanded", "true");
  await row.getByRole("button", { name: `管理目录“${second.name}”` }).click();
  await page.getByRole("menuitem", { name: "编辑名称" }).click();
  await row.getByRole("textbox", { name: "游戏目录", exact: true }).fill(`${second.name} 已编辑`);
  await row.getByRole("button", { name: "保存", exact: true }).click();
  await expect(row.getByRole("heading", { name: `${second.name} 已编辑`, exact: true })).toBeVisible();
  await row.getByRole("checkbox").click();
  await expect(row.getByRole("checkbox")).not.toBeChecked();
  await page.getByRole("combobox", { name: "启用状态", exact: true }).selectOption("DISABLED");
  await expect(row).toBeVisible();
  await expect(page.locator(`#directory-${first.id}`)).toHaveCount(0);
  await page.getByRole("combobox", { name: "启用状态", exact: true }).selectOption("ALL");
  await row.getByRole("checkbox").click();
  await expect(row.getByRole("checkbox")).toBeChecked();
  await page.getByRole("button", { name: "全部展开" }).click();
  await expect(page.getByRole("button", { name: "排序说明" })).toHaveCount(0);
  await expect(page.getByRole("columnheader", { name: "顺序", exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await screenshot(page, testInfo, "expanded");
});
