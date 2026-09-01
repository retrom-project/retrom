import { mkdirSync } from "node:fs";
import path from "node:path";
import { expect, test, type Page, type TestInfo } from "@playwright/test";
import axe from "axe-core";

const tagName = "掌机精选";
const renamedTagName = "掌机典藏";

test.beforeEach(async ({ page }) => {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const response = await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" }, headers: { Origin: origin },
  });
  expect(response.ok()).toBe(true);
});

function evidencePath(testInfo: TestInfo, name: string) {
  const caseDirectory = process.env.RETROM_ACCEPTANCE_CASE_DIR;
  if (!caseDirectory) {return testInfo.outputPath(name);}
  const screenshots = path.join(caseDirectory, "screenshots");
  mkdirSync(screenshots, { recursive: true });
  return path.join(screenshots, name);
}

async function expectNoOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
}

async function expectNoSeriousAxeViolations(page: Page) {
  await page.evaluate(axe.source);
  const violations = await page.evaluate(async () => {
    const axeAPI = (window as typeof window & {
      axe: { run: (root: Document, options: object) => Promise<{ violations: Array<{ id: string; impact: string | null; nodes: unknown[] }> }> };
    }).axe;
    const result = await axeAPI.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    return result.violations.filter((violation) => violation.impact === "serious" || violation.impact === "critical");
  });
  expect(violations, `axe serious/critical violations: ${JSON.stringify(violations)}`).toEqual([]);
}

async function expectFocusedControl(page: Page, name: string) {
  await expect.poll(() => page.evaluate(() => {
    const active = document.activeElement;
    if (!(active instanceof HTMLElement)) {return "non-html";}
    return active.getAttribute("aria-label") || active.textContent?.trim() || `${active.tagName}.${active.className}`;
  })).toBe(name);
}

test("ACC-TAG-005 tag administration, assignment, search, projection, responsive layout and accessibility", async ({ page }, testInfo) => {
  await page.goto("/admin/tags");
  const commonNames = ["动作冒险", "飞行射击", "格斗对战", "角色扮演", "模拟经营", "即时战略", "体育竞技", "益智解谜", "光枪射击", "生存恐怖"];
  await page.getByRole("button", { name: "添加常用标签" }).click();
  await expect(page.getByText(/常用标签.*(?:已存在|已全部存在)/)).toBeVisible();
  for (const name of commonNames) {await expect(page.getByRole("rowheader", { name })).toBeVisible();}
  await page.getByRole("button", { name: "添加常用标签" }).click();
  await expect(page.getByText("10 个常用标签已全部存在。")).toBeVisible();
  const createTrigger = page.getByRole("button", { name: "新建标签" });
  await createTrigger.focus();
  await page.keyboard.press("Enter");
  const createSheet = page.getByRole("dialog", { name: "新建标签" });
  await expect(createSheet.getByRole("textbox", { name: "标签名称" })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(createSheet).toBeHidden();
  await expect(createTrigger).toBeFocused();

  await page.keyboard.press("Enter");
  await createSheet.getByRole("textbox", { name: "标签名称" }).fill(`  ${tagName}  `);
  const previewBounds = await createSheet.locator(".tag-normalized-preview").boundingBox();
  const helpBounds = await createSheet.locator(".tag-editor-help").boundingBox();
  expect(previewBounds).not.toBeNull();
  expect(helpBounds).not.toBeNull();
  expect(helpBounds!.y - (previewBounds!.y + previewBounds!.height)).toBeGreaterThanOrEqual(8);
  const createSave = createSheet.getByRole("button", { name: "保存标签" });
  await expect(createSheet.getByRole("textbox", { name: "标签名称" })).toBeFocused();
  await page.keyboard.press("Tab");
  await expectFocusedControl(page, "取消");
  await page.keyboard.press("Tab");
  await expectFocusedControl(page, "保存标签");
  await createSave.press("Enter");
  const createdRow = page.getByRole("row").filter({ has: page.getByRole("rowheader", { name: tagName }) });
  await expect(createdRow).toBeVisible();

  const gamesResponse = await page.request.get("/api/v1/admin/games?limit=100");
  expect(gamesResponse.ok()).toBe(true);
  const games = await gamesResponse.json() as { items: Array<{ gameId: string; title: string }> };
  expect(games.items.length).toBeGreaterThan(0);
  const game = games.items[0];

  await page.goto(`/admin/games/${game.gameId}`);
  const picker = page.getByRole("combobox", { name: "标签" });
  const pickerControlStyle = await picker.evaluate((element) => {
    const style = window.getComputedStyle(element);
    const bounds = element.getBoundingClientRect();
    return {
      backgroundColor: style.backgroundColor,
      borderRadius: Number.parseFloat(style.borderTopLeftRadius),
      borderStyle: style.borderTopStyle,
      borderWidth: Number.parseFloat(style.borderTopWidth),
      height: bounds.height,
      paddingLeft: Number.parseFloat(style.paddingLeft),
    };
  });
  expect(pickerControlStyle).toMatchObject({
    backgroundColor: "rgb(255, 255, 255)",
    borderStyle: "solid",
    borderWidth: 1,
  });
  expect(pickerControlStyle.height).toBeGreaterThanOrEqual(44);
  expect(pickerControlStyle.borderRadius).toBeGreaterThanOrEqual(9);
  expect(pickerControlStyle.paddingLeft).toBeGreaterThanOrEqual(12);
  await picker.focus();
  await picker.fill(tagName);
  await page.keyboard.press("Enter");
  await expect(page.getByRole("button", { name: `移除标签“${tagName}”` })).toBeVisible();
  await page.getByRole("button", { name: "更新标签" }).click();
  await expect(page.getByText("游戏标签已更新。", { exact: true })).toBeVisible();

  await page.goto("/admin/tags");
  const assignedRow = page.getByRole("row").filter({ has: page.getByRole("rowheader", { name: tagName }) });
  await expect(assignedRow.getByRole("link", { name: "1 / 0" })).toBeVisible();
  await assignedRow.getByRole("button", { name: "编辑" }).click();
  const editSheet = page.getByRole("dialog", { name: "编辑标签" });
  const editName = editSheet.getByRole("textbox", { name: "标签名称" });
  await editName.fill(renamedTagName);
  await editSheet.getByRole("button", { name: "保存标签" }).click();
  await expect(page.getByRole("rowheader", { name: renamedTagName })).toBeVisible();

  await page.goto(`/library?q=${encodeURIComponent(renamedTagName)}`);
  await expect(page.getByRole("searchbox", { name: "搜索游戏" })).toHaveValue(renamedTagName);
  const card = page.locator(".library-game-card").filter({ hasText: game.title }).first();
  await expect(card).toBeVisible();
  await expect(card.getByText(renamedTagName, { exact: true })).toBeVisible();
  const exactTag = await page.getByRole("combobox", { name: "标签" }).locator("option", { hasText: renamedTagName }).getAttribute("value");
  expect(exactTag).toBeTruthy();
  await page.getByRole("combobox", { name: "标签" }).selectOption(exactTag!);
  await expect(page).toHaveURL(new RegExp(`tagId=${encodeURIComponent(exactTag!)}`));
  await page.reload();
  await expect(page.getByRole("combobox", { name: "标签" })).toHaveValue(exactTag!);
  await card.getByRole("link").first().click();
  await expect(page.getByRole("link", { name: `查看标签“${renamedTagName}”下的游戏` })).toBeVisible();

  const viewports = [
    { width: 390, height: 844, label: "390x844" },
    { width: 1280, height: 800, label: "1280x800" },
    { width: 2560, height: 1440, label: "2560x1440" },
    { width: 3840, height: 2160, label: "3840-css-ultrawide" },
  ];
  for (const viewport of viewports) {
    await page.setViewportSize(viewport);
    await page.goto("/admin/tags");
    await expect(page.getByRole("heading", { name: "标签管理" })).toBeVisible();
    await expect(page.getByRole("rowheader", { name: renamedTagName }).or(page.getByRole("heading", { name: renamedTagName }))).toBeVisible();
    await expectNoOverflow(page);
    await page.screenshot({ path: evidencePath(testInfo, `tags-${viewport.label}.png`), fullPage: true });
  }
  await expectNoSeriousAxeViolations(page);

  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto("/admin/tags");
  const renamedRow = page.getByRole("row").filter({ has: page.getByRole("rowheader", { name: renamedTagName }) });
  await renamedRow.getByRole("button", { name: "删除" }).click();
  const deleteDialog = page.getByRole("alertdialog", { name: "删除标签" });
  await expect(deleteDialog.getByRole("button", { name: "取消" })).toBeFocused();
  await deleteDialog.getByRole("textbox", { name: /输入完整名称/ }).fill(renamedTagName);
  await deleteDialog.getByRole("button", { name: "删除标签" }).click();
  await expect(page.getByRole("rowheader", { name: renamedTagName })).toBeHidden();

  await page.goto(`/library?q=${encodeURIComponent(renamedTagName)}`);
  await expect(page.getByRole("heading", { name: "没有找到游戏" })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "标签" }).locator("option", { hasText: renamedTagName })).toHaveCount(0);
});
