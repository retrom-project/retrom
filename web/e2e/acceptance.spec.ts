import { expect, test } from "@playwright/test";
import { evidencePath, expectNoTextArrowsInInteractiveControls, noPageOverflow, pageCanvasGaps, pngDimensions, type HorizontalGaps } from "./acceptance-support";
import { registerRuntimeAcceptanceTests } from "./acceptance-runtime-cases";
import { verifyUserDesktopLayouts } from "./acceptance-user-layout";

test.beforeEach(async ({ page }, testInfo) => {
  const multiViewport = /^ACC-UI-00[56]\\b/.test(testInfo.title);
  test.skip(!multiViewport && testInfo.project.name !== "chrome-1280", "此状态型 Case 只消费一次共享验收夹具");
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000";
  const response = await page.request.post("/api/v1/auth/login", { data: { username: "test", password: "test" }, headers: { Origin: origin } });
  expect(response.ok()).toBe(true);
});

test("ACC-UI-001 authenticated navigation exposes the administrator entry", async ({ page }, testInfo) => {
  await page.goto("/");
  const navigation = page.getByRole("navigation", { name: "主要导航" });
  await expect(navigation.getByRole("link")).toHaveCount(6);
  await expect(navigation.getByRole("link")).toHaveText([
    "首页", "游戏库", "我的存档", "我的收藏", "最近游玩", "联机游玩"
  ]);
  await navigation.getByRole("link", { name: "最近游玩" }).click();
  await expect(page).toHaveURL(/\/recent$/);
  await navigation.getByRole("link", { name: "游戏库" }).click();
  await expect(page).toHaveURL(/\/library$/);
  const firstGame = page.locator(".library-game-card").first();
  if (await firstGame.count()) {
    await firstGame.getByRole("link").first().click();
    await expect(page).toHaveURL(/\/games\/[0-9a-f-]+$/);
    await expect(page.getByRole("navigation", { name: "主要导航" }).getByRole("link")).toHaveCount(6);
  }
  const userSidebarFoot = page.locator(".sidebar-foot");
  await expect(userSidebarFoot.locator(".sidebar-account-row .connection")).toHaveCount(1);
  expect(await userSidebarFoot.locator(":scope > *").evaluateAll((elements) => elements.map((element) => element.className))).toEqual(["sidebar-account-row", "context-switch"]);
  await page.getByRole("link", { name: "管理后台" }).click();
  await expect(page).toHaveURL(/\/admin\/imports$/);
  await expect(page.getByRole("link", { name: "返回用户侧" })).toBeVisible();
  const adminSidebarFoot = page.locator(".sidebar-foot");
  await expect(adminSidebarFoot.locator(".sidebar-account-row .connection")).toHaveCount(1);
  expect(await adminSidebarFoot.locator(":scope > *").evaluateAll((elements) => elements.map((element) => element.className))).toEqual(["sidebar-account-row", "context-switch"]);
  await page.screenshot({ path: evidencePath(testInfo, "user-navigation.png"), fullPage: true });
});

test("ACC-UI-002 import parent and child routes preserve browser history", async ({ page }, testInfo) => {
  const routes = [
    ["/admin/imports", "游戏入库"],
    ["/admin/imports/new", "导入游戏"],
    ["/admin/imports/server", "本地扫描"],
    ["/admin/imports/tasks", "任务进度"],
    ["/admin/reviews", "待审核"],
    ["/admin/reviews/history", "审核历史"],
  ] as const;
  for (const [index, [route, label]] of routes.entries()) {
    await page.goto(route);
    const navigation = page.getByRole("navigation", { name: "主要导航" });
    await expect(navigation.getByRole("link").nth(1)).toHaveText("导入游戏");
    await expect(navigation.getByRole("link").nth(2)).toHaveText("本地扫描");
    await expect(navigation.getByRole("link").nth(3)).toHaveText("任务进度");
    await expect(navigation.getByRole("link", { name: "游戏入库", exact: true })).toHaveClass(index === 0 ? /is-active/ : /is-context/);
    await expect(navigation.getByRole("link", { name: label, exact: true })).toHaveClass(/is-active/);
    if (index > 0) {await expect(navigation.getByRole("link", { name: label, exact: true })).toHaveClass(/nav-child/);}
    await page.screenshot({ path: evidencePath(testInfo, `import-route-${index + 1}.png`), fullPage: true });
  }
  await page.goBack();
  await expect(page).toHaveURL(/\/admin\/reviews$/);
  await page.goForward();
  await expect(page).toHaveURL(/\/admin\/reviews\/history$/);
});

test("ACC-UI-003 library filters and game detail use URL state", async ({ page }, testInfo) => {
  await page.goto("/");
  await expect(page.locator("[data-home-layer]")).toHaveCount(5);
  await expect(page.getByText("我的资料库", { exact: true })).toBeVisible();
  const latestLayer = page.locator('[data-home-layer="3"]');
  await expect(latestLayer.getByRole("heading", { name: "最新添加" })).toBeVisible();
  const latestCardCount = await latestLayer.locator(".home-recent-card").count();
  expect(latestCardCount).toBeGreaterThan(0);
  expect(latestCardCount).toBeLessThanOrEqual(10);
  await latestLayer.getByRole("link", { name: "查看游戏库", exact: true }).click();
  await expect(page).toHaveURL(/\/library\?sort=ADDED_DESC$/);
  await expect(page.getByRole("combobox", { name: "排列顺序" })).toHaveValue("ADDED_DESC");
  await page.goto("/library");
  await expect(page.locator(".library-toolbar")).toBeVisible();
  const desktopFilterTops = await Promise.all([
    page.getByRole("searchbox", { name: "搜索游戏" }),
    page.getByRole("combobox", { name: "游戏集合" }),
    page.getByRole("combobox", { name: "标签" }),
    page.getByRole("combobox", { name: "排列顺序" }),
  ].map(async (control) => (await control.boundingBox())?.y));
  expect(desktopFilterTops.every((top) => top !== undefined)).toBe(true);
  expect(Math.max(...desktopFilterTops as number[]) - Math.min(...desktopFilterTops as number[])).toBeLessThanOrEqual(1);
  const toolbarHeight = await page.locator(".library-toolbar").evaluate((element) => element.getBoundingClientRect().height);
  await page.evaluate(() => { (window as typeof window & { __retromSearchMarker?: string }).__retromSearchMarker = "preserved"; });
  await page.getByRole("searchbox", { name: "搜索游戏" }).fill("Sudoku");
  const gbaPlatform = page.getByRole("button", { name: /^Game Boy Advance \d+$/ });
  await gbaPlatform.click();
  const directory = page.getByRole("combobox", { name: "游戏集合" });
  await expect(directory).toBeVisible();
  const directoryValue = await directory.locator("option").nth(1).getAttribute("value");
  expect(directoryValue).toBeTruthy();
  await directory.selectOption(directoryValue!);
  await expect(page).toHaveURL(/q=Sudoku/);
  await expect(page).toHaveURL(/platformId=gba/);
  await expect(page).toHaveURL(/platformInstanceId=/);
  expect(await page.locator(".library-toolbar").evaluate((element) => element.getBoundingClientRect().height)).toBe(toolbarHeight);
  expect(await page.evaluate(() => (window as typeof window & { __retromSearchMarker?: string }).__retromSearchMarker)).toBe("preserved");
  await page.reload();
  await expect(page.getByRole("searchbox", { name: "搜索游戏" })).toHaveValue("Sudoku");
  await expect(page.getByRole("button", { name: /^Game Boy Advance \d+$/ })).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByRole("combobox", { name: "游戏集合" })).toHaveValue(directoryValue!);
  await expect(page.locator(".library-result-count")).toContainText("1");
  const game = page.locator(".library-game-card").first();
  await expect(game).toBeVisible();
  await game.getByRole("link").first().click();
  await expect(page).toHaveURL(/\/games\/[0-9a-f-]+$/);
  await expect(page.getByRole("button", { name: "开始游戏" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "从游戏开头开始" })).toBeVisible();
  await page.setViewportSize({ width: 2560, height: 1440 });
  const descriptionLayout = await page.locator(".game-detail-description").evaluate((element) => {
    const box = element.getBoundingClientRect();
    const main = element.parentElement!.getBoundingClientRect();
    const style = getComputedStyle(element);
    return { width: box.width, mainWidth: main.width, clientHeight: element.clientHeight, scrollHeight: element.scrollHeight, lineClamp: style.webkitLineClamp };
  });
  expect(descriptionLayout.width / descriptionLayout.mainWidth).toBeGreaterThanOrEqual(0.98);
  expect(descriptionLayout.scrollHeight).toBeLessThanOrEqual(descriptionLayout.clientHeight + 1);
  expect(["none", "0", ""]).toContain(descriptionLayout.lineClamp);
  const heroHeight = await page.locator(".game-detail-hero").evaluate((element) => element.getBoundingClientRect().height);
  await page.getByRole("button", { name: /更换/ }).click();
  await expect(page.getByRole("alertdialog", { name: "更换运行方式" })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "运行引擎" }).locator("option:checked")).toContainText("推荐");
  expect(await page.locator(".game-detail-hero").evaluate((element) => element.getBoundingClientRect().height)).toBe(heroHeight);
  await page.screenshot({ path: evidencePath(testInfo, "library-detail-flow.png"), fullPage: true });
});

test("ACC-UI-004 loading, empty, retryable error, warning, and blocker states are explicit", async ({ page }, testInfo) => {
  let releaseLibrary: () => void = () => undefined;
  const libraryGate = new Promise<void>((resolve) => { releaseLibrary = resolve; });
  await page.route((url) => url.pathname === "/library", async (route) => {
    await libraryGate;
    await route.continue();
  });
  await page.goto("/");
  const libraryLink = page.getByRole("link", { name: "游戏库", exact: true });
  const navigation = libraryLink.click();
  await expect(page.getByLabel("正在加载")).toBeVisible();
  await page.screenshot({ path: evidencePath(testInfo, "state-loading.png"), fullPage: true });
  releaseLibrary();
  await navigation;
  await page.unrouteAll({ behavior: "wait" });

  await page.goto("/library?q=definitely-no-matching-game");
  await expect(page.getByRole("heading", { name: "没有找到游戏" })).toBeVisible();
  await expect(page.getByText("清除筛选", { exact: false })).toBeVisible();
  await page.screenshot({ path: evidencePath(testInfo, "state-empty.png"), fullPage: true });

  await page.goto("/admin/reviews?sort=INVALID");
  await expect(page.getByRole("heading", { name: "暂时无法读取数据" })).toBeVisible();
  await expect(page.getByRole("button", { name: "重新加载" })).toBeVisible();
  await page.screenshot({ path: evidencePath(testInfo, "state-error.png"), fullPage: true });

  await page.goto("/admin/bios?scope=FULL_CATALOG");
  await expect(page.getByRole("heading", { name: "BIOS 文件" })).toBeVisible();
  const gbaRow = page.getByRole("row").filter({ hasText: "gba_bios.bin" });
  await gbaRow.locator('input[type="file"]').setInputFiles({ name: "gba_bios.bin", mimeType: "application/octet-stream", buffer: Buffer.from("retrom-invalid-bios\n") });
  await expect(gbaRow.getByText("校验值不一致", { exact: true })).toBeVisible();
  await expect(gbaRow.getByText("期望 MD5", { exact: true })).toBeVisible();
  await expect(gbaRow.getByText("当前 MD5", { exact: true })).toBeVisible();
  await page.screenshot({ path: evidencePath(testInfo, "state-warning.png"), fullPage: true });

  const blockerRow = page.getByRole("row").filter({ hasText: "缺少文件" }).filter({ hasText: "必需" }).first();
  await expect(blockerRow.getByText("必需", { exact: false })).toBeVisible();
  await expect(blockerRow.getByRole("button", { name: "选择 BIOS 文件" })).toBeVisible();
  await page.screenshot({ path: evidencePath(testInfo, "state-blocker.png"), fullPage: true });
});

test("ACC-UI-005 user desktop layouts scale at all required viewports", async ({ page }, testInfo) => {
  await verifyUserDesktopLayouts(page, testInfo);
});

test("ACC-UI-005 regression: sparse home rails keep game cards within desktop width caps", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator('[data-home-layer="2"] h2')).toHaveText("最近游玩");
  await expect(page.locator('[data-home-layer="3"] h2')).toHaveText("最新添加");
  const rails = page.locator(".home-recent-rail");
  expect(await rails.count()).toBeGreaterThan(0);
  expect(await rails.count()).toBeLessThanOrEqual(2);
  if (await page.locator('[data-home-layer="2"] .home-recent-card').count() === 0) {
    await expect(page.locator('[data-home-layer="2"] .home-inline-empty')).toBeVisible();
  }
  const layout = await rails.evaluateAll((railElements) => ({
    cardWidths: railElements.flatMap((rail) =>
      [...rail.querySelectorAll<HTMLElement>(".home-recent-card")].map((card) => card.getBoundingClientRect().width)),
    widthCap: window.matchMedia("(min-width: 2600px) and (min-height: 1600px)").matches ? 560 : 480,
  }));
  expect(layout.cardWidths.length).toBeGreaterThan(0);
  expect(Math.max(...layout.cardWidths)).toBeLessThanOrEqual(layout.widthCap + 1);
});

test("ACC-UI-006 admin pages remain reachable at desktop breakpoints", async ({ page }, testInfo) => {
  const routes = [
    ["/admin/imports", ".import-workflow-page"], ["/admin/imports/new", ".import-wizard"],
    ["/admin/imports/server", ".page-layout-admin"], ["/admin/imports/tasks", ".import-workflow-page"],
    ["/admin/reviews", ".import-workflow-page"], ["/admin/reviews/history", ".import-workflow-page"],
    ["/admin/games", ".page-header"], ["/admin/platform-instances", ".platform-directory-manager"],
    ["/admin/users", ".user-admin-page"], ["/admin/bios", ".page-layout-admin"],
  ] as const;
  let sharedPageGaps: HorizontalGaps | null = null;
  for (const [route, selector] of routes) {
    await page.goto(route);
    await expect(page.locator(".page-header")).toBeVisible();
    await noPageOverflow(page);
    await expectNoTextArrowsInInteractiveControls(page);
    await expect(page.getByRole("main")).toBeVisible();
    const gaps = await pageCanvasGaps(page, selector);
    if (sharedPageGaps) {
      expect(Math.abs(gaps.left - sharedPageGaps.left)).toBeLessThanOrEqual(1);
      expect(Math.abs(gaps.right - sharedPageGaps.right)).toBeLessThanOrEqual(1);
    } else {
      sharedPageGaps = gaps;
    }
  }
  await page.goto("/admin/imports/new");
  const dropzoneAlignment = await page.locator(".dropzone").evaluate((dropzone) => {
    const dropzoneBox = dropzone.getBoundingClientRect();
    const actionsBox = dropzone.querySelector(".dropzone-actions")?.getBoundingClientRect();
    return actionsBox ? Math.abs((dropzoneBox.left + dropzoneBox.width / 2) - (actionsBox.left + actionsBox.width / 2)) : null;
  });
  expect(dropzoneAlignment).not.toBeNull();
  expect(dropzoneAlignment!).toBeLessThanOrEqual(1);
  await page.goto("/admin/imports/tasks");
  await expect(page.getByRole("heading", { name: "普通任务进度", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "本地扫描任务", exact: true })).toHaveAttribute("href", "/admin/imports/server");
  await expect(page.getByText("查看浏览器上传或重新配置产生的导入批次；Pegasus 目录按顶层批次统一显示在本地扫描。", { exact: true })).toBeVisible();
  await expect(page.getByText("技术详情", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "查看多盘详情" })).toHaveCount(0);
  const taskGridColumns = await page.locator(".import-task-card").evaluateAll((cards) => cards.map((card) => getComputedStyle(card).gridTemplateColumns));
  expect(new Set(taskGridColumns).size).toBeLessThanOrEqual(1);
  await page.goto("/admin/games");
  await expect(page.getByRole("heading", { name: "游戏管理", exact: true })).toBeVisible();
  await expect(page.getByText("信息版本", { exact: true })).toHaveCount(0);
  const adminGameRow = page.locator(".admin-game-table tbody tr").first();
  await expect(adminGameRow).toBeVisible();
  expect((await adminGameRow.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(84);
  await expect(page.getByRole("region", { name: "游戏管理摘要" })).toBeVisible();
  await page.evaluate(() => window.history.replaceState({ marker: "admin-games" }, "", window.location.href));
  await page.getByRole("searchbox", { name: "搜索游戏" }).fill("Sudoku");
  await expect(page.locator(".admin-game-table tbody tr")).toHaveCount(1);
  await expect(page.locator(".admin-game-table tbody tr")).toContainText("Sudoku");
  expect(await page.evaluate(() => window.history.state?.marker)).toBe("admin-games");
  await page.getByRole("searchbox", { name: "搜索游戏" }).fill("");
  expect((await page.request.get("/admin/bios/dats")).status()).toBe(404);
  await page.goto("/admin/platform-instances");
  await expect(page.getByRole("heading", { name: "游戏目录", exact: true })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "主要导航" }).getByText("游戏目录", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "✓ 推荐目录已补全" })).toBeDisabled();
  await expect(page.getByRole("columnheader", { name: "启用状态" })).toBeVisible();
  await expect(page.getByText("FDS 游戏", { exact: true })).toHaveCount(0);
  await expect(page.getByText("MAME 2003 游戏", { exact: true })).toHaveCount(0);
  const nesDirectory = page.locator(".platform-directory-row").filter({ hasText: "NES 游戏" });
  await expect(nesDirectory.getByText(".fds", { exact: true })).toHaveCount(1);
  const mamePlusDirectory = page.locator(".platform-directory-row").filter({ hasText: "MAME 2003 Plus 游戏" });
  await expect(mamePlusDirectory.getByText(".zip", { exact: true })).toHaveCount(1);
  const directoryRowHeights = await page.locator(".platform-directory-row").evaluateAll((rows) => rows.map((row) => row.getBoundingClientRect().height));
  expect(directoryRowHeights.length).toBeGreaterThan(0);
  expect(directoryRowHeights.every((height) => height >= 87 && height <= 90)).toBe(true);
  const populatedDirectory = page.locator(".platform-directory-row").filter({ hasText: /[1-9]\d* 款/ }).first();
  if (await populatedDirectory.count()) {await expect(populatedDirectory.getByRole("checkbox", { name: /启用状态/ })).toBeEnabled();}
  const descriptionRow = page.locator(".platform-directory-row").first();
  if (await descriptionRow.count()) {
    const before = await descriptionRow.evaluate((element) => element.getBoundingClientRect().height);
    await descriptionRow.getByRole("button", { name: /管理目录/ }).click();
    await descriptionRow.getByRole("menuitem", { name: "编辑说明" }).click();
    await expect(descriptionRow.getByRole("textbox", { name: "给用户看的说明" })).toHaveAttribute("rows", "1");
    const after = await descriptionRow.evaluate((element) => element.getBoundingClientRect().height);
    expect(Math.abs(after - before)).toBeLessThanOrEqual(4);
    await descriptionRow.getByRole("button", { name: "取消修改说明" }).click();
  }
  await page.getByRole("button", { name: "新建游戏目录" }).click();
  await expect(page.getByRole("dialog", { name: "新建游戏目录" })).toBeVisible();
  await expect(page.getByText("新建平台实例", { exact: true })).toHaveCount(0);
  await page.getByRole("dialog", { name: "新建游戏目录" }).getByRole("button", { name: "关闭", exact: true }).click();
  const games = await page.request.get("/api/v1/admin/games?limit=100");
  const payload = await games.json() as { items: Array<{ gameId: string }> };
  if (payload.items[0]) {
    await page.goto(`/admin/games/${payload.items[0].gameId}`);
    await pageCanvasGaps(page, ".admin-game-detail");
    for (const heading of ["发布信息", "媒体", "游戏文件与运行环境", "管理操作", "从游戏库移除"]) {
      await expect(page.getByRole("heading", { name: heading })).toBeVisible();
    }
    for (const omittedTag of ["媒体资源", "运行状态正常", "维护工具", "危险区域"]) {
      await expect(page.getByText(omittedTag, { exact: true })).toHaveCount(0);
    }
    const adminCoverRatio = await page.locator(".admin-game-cover-frame").evaluate((element) => {
      const box = element.getBoundingClientRect();
      return box.width / box.height;
    });
    expect(Math.abs(adminCoverRatio - 0.75)).toBeLessThanOrEqual(0.01);
    const coverBottomGap = await page.locator(".admin-game-media-grid").evaluate((element) => {
      const body = element.getBoundingClientRect();
      const cover = element.querySelector<HTMLElement>(".admin-game-cover-frame")!.getBoundingClientRect();
      return body.bottom - Number.parseFloat(getComputedStyle(element).paddingBottom) - cover.bottom;
    });
    expect(Math.abs(coverBottomGap)).toBeLessThanOrEqual(1);
    await expect(page.getByRole("button", { name: "保存新版本" })).toBeDisabled();
    await noPageOverflow(page);
  }
  const adminLayoutScreenshot = await page.screenshot({
    path: evidencePath(testInfo, "admin-layout.png"),
    fullPage: testInfo.project.name !== "chrome-4k-150",
  });
  if (testInfo.project.name === "chrome-4k-150") {
    expect(pngDimensions(adminLayoutScreenshot)).toEqual({ width: 3840, height: 2160 });
  }
});

test("ACC-UI-007 keyboard focus and reduced motion remain explicit", async ({ page }, testInfo) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/library");
  await page.keyboard.press("Tab");
  expect(await page.evaluate(() => document.activeElement?.tagName)).not.toBe("BODY");
  const reducedDuration = await page.locator(".library-game-card").first().evaluate((element) => getComputedStyle(element).transitionDuration);
  expect(Number.parseFloat(reducedDuration)).toBeLessThanOrEqual(0.01);
  const focusable = page.locator("a,button,input,select");
  expect(await focusable.count()).toBeGreaterThan(5);
  for (const control of await page.locator("main button:visible").all()) {await expect(control).toHaveAccessibleName(/.+/);}
  await page.screenshot({ path: evidencePath(testInfo, "keyboard-reduced-motion.png"), fullPage: true });
});

test("ACC-UI-008 large review queue preserves filters, pagination, draft safety, and decisions", async ({ page }, testInfo) => {
  const primaryJob = "20000000-0000-7000-8000-000000000001";
  const itemId = (number: number) => `30000000-0000-7000-8001-${String(number).padStart(12, "0")}`;
  await page.goto("/admin/imports/tasks");
  const primaryRow = page.locator(".import-task-card").filter({ hasText: "60 个条目" });
  await expect(primaryRow).toBeVisible();
  await primaryRow.getByRole("link", { name: "查看待审核" }).click();
  await expect(page).toHaveURL(new RegExp(`importJobId=${primaryJob}`));
  await expect(page.getByRole("textbox", { name: "导入批次" })).toHaveValue(primaryJob);
  const rows = page.locator("[data-review-item]");
  await expect(rows).toHaveCount(20);
  await page.getByRole("button", { name: "继续加载" }).click();
  await expect(rows).toHaveCount(40);
  await page.getByRole("button", { name: "继续加载" }).click();
  await expect(rows).toHaveCount(60);
  expect(new Set(await rows.evaluateAll((elements) => elements.map((element) => element.getAttribute("data-review-item")))).size).toBe(60);
  await expect(rows.first()).toContainText("可以发布");
  await expect(page.getByRole("button", { name: "快速审批" })).toBeVisible();

  await page.getByRole("link", { name: "清除" }).click();
  await expect(page).toHaveURL(/\/admin\/reviews$/);
  await expect(rows).toHaveCount(20);
  await page.getByRole("button", { name: "继续加载" }).click();
  await expect(rows).toHaveCount(40);
  await page.getByRole("button", { name: "继续加载" }).click();
  await expect(rows).toHaveCount(60);
  await page.getByRole("button", { name: "继续加载" }).click();
  await expect(rows).toHaveCount(63);
  await page.goBack();
  await expect(page).toHaveURL(new RegExp(`importJobId=${primaryJob}`));
  await expect(rows).toHaveCount(60);

  const item57 = page.locator(`[data-review-item="${itemId(57)}"]`).getByRole("link", { name: "审核条目" });
  await item57.focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(new RegExp(`/admin/reviews/${itemId(57)}`));
  // Keep a supplemental ultra-wide CSS breakpoint check. Physical 4K evidence
  // is captured by the chrome-4k-150 project at 2560x1440 CSS pixels and DPR 1.5.
  await page.setViewportSize({ width: 3840, height: 2160 });
  await expect(page.getByRole("heading", { name: "审核决定" })).toBeVisible();
  let reviewPopupCount = 0;
  const reviewPreviewRequests: string[] = [];
  page.on("popup", async (popup) => {
    reviewPopupCount += 1;
    await popup.close();
  });
  page.on("request", (request) => {
    if (request.url().endsWith(`/api/v1/admin/reviews/${itemId(57)}/previews`)) {reviewPreviewRequests.push(request.url());}
  });
  const refreshedReview = page.waitForResponse((response) =>
    response.request().method() === "GET" && response.url().endsWith(`/api/v1/admin/reviews/${itemId(57)}`));
  await page.getByRole("button", { name: "重新运行检查" }).click();
  await refreshedReview;
  expect(reviewPopupCount).toBe(0);
  expect(reviewPreviewRequests).toEqual([]);
  expect(await page.locator(".review-workflow-columns").evaluate((element) => getComputedStyle(element).gridTemplateColumns.split(" ").length)).toBe(2);
  const screenshotLayout = await page.locator(".review-workflow-summary-card").evaluate((element) => {
    const clone = element.cloneNode(true) as HTMLElement;
    clone.style.position = "fixed";
    clone.style.left = "-10000px";
    clone.style.top = "0";
    clone.style.width = `${element.getBoundingClientRect().width}px`;
    document.body.append(clone);
    const screenshot = clone.querySelector<HTMLElement>(".review-runtime-screenshot")!;
    const placeholderCardHeight = clone.getBoundingClientRect().height;
    const placeholderScreenshotHeight = screenshot.getBoundingClientRect().height;
    const image = document.createElement("img");
    image.width = 1920;
    image.height = 1080;
    image.alt = "外层尺寸回归截图";
    screenshot.lastElementChild?.replaceWith(image);
    const imageCardHeight = clone.getBoundingClientRect().height;
    const imageScreenshotHeight = screenshot.getBoundingClientRect().height;
    const placeholderCardWidth = clone.getBoundingClientRect().width;
    const placeholderScreenshotWidth = screenshot.getBoundingClientRect().width;
    clone.remove();
    return { placeholderCardHeight, placeholderCardWidth, placeholderScreenshotHeight, placeholderScreenshotWidth, imageCardHeight, imageScreenshotHeight };
  });
  expect(screenshotLayout.placeholderCardHeight).toBe(196);
  expect(screenshotLayout.imageCardHeight).toBe(screenshotLayout.placeholderCardHeight);
  expect(screenshotLayout.imageScreenshotHeight).toBe(screenshotLayout.placeholderScreenshotHeight);
  expect(screenshotLayout.placeholderScreenshotWidth / screenshotLayout.placeholderCardWidth).toBeLessThanOrEqual(.18);
  const validationOverflow = await page.locator(".review-workflow-capability").evaluate((element) => {
    const detail = document.createElement("div");
    detail.className = "review-validation-guidance";
    const list = document.createElement("ul");
    for (let index = 0; index < 40; index += 1) {
      const item = document.createElement("li");
      item.textContent = `missing-entry-${index + 1}.bin 缺失`;
      list.append(item);
    }
    detail.append(list);
    element.append(detail);
    const bounds = detail.getBoundingClientRect();
    const computed = getComputedStyle(detail);
    const result = { height: bounds.height, clientHeight: detail.clientHeight, scrollHeight: detail.scrollHeight, overflowY: computed.overflowY };
    detail.remove();
    return result;
  });
  expect(validationOverflow.height).toBeLessThanOrEqual(361);
  expect(validationOverflow.scrollHeight).toBeGreaterThan(validationOverflow.clientHeight);
  expect(validationOverflow.overflowY).toBe("auto");
  const reviewLayout = await page.locator(".review-workflow-columns").evaluate((element) => {
    const left = element.querySelector<HTMLElement>(".review-workflow-left")!.getBoundingClientRect();
    const metadata = element.querySelector<HTMLElement>(".review-workflow-metadata")!.getBoundingClientRect();
    const fields = element.querySelector<HTMLElement>(".review-workflow-metadata-fields")!.getBoundingClientRect();
    const tagEditor = element.querySelector<HTMLElement>(".review-tag-editor")!;
    const cover = element.querySelector<HTMLElement>(".review-workflow-cover-side")!.getBoundingClientRect();
    const coverImage = element.querySelector<HTMLElement>(".review-workflow-cover-side .review-cover-upload")!.getBoundingClientRect();
    const coverStyle = getComputedStyle(element.querySelector<HTMLElement>(".review-workflow-cover-side")!);
    const description = element.querySelector<HTMLTextAreaElement>(".review-workflow-metadata-fields textarea")!;
    const descriptionLabel = description.closest("label")!;
    const descriptionText = document.createRange();
    descriptionText.selectNodeContents(descriptionLabel.firstChild!);
    const descriptionGap = description.getBoundingClientRect().top - descriptionText.getBoundingClientRect().bottom;
    return { leftHeight: left.height, metadataHeight: metadata.height, fieldsRight: fields.right, coverLeft: cover.left, coverWidth: cover.width, coverBottomGap: cover.bottom - coverImage.bottom - Number.parseFloat(coverStyle.paddingBottom), descriptionGap, tagInsideFields: tagEditor.parentElement?.classList.contains("review-workflow-metadata-fields") ?? false };
  });
  expect(Math.abs(reviewLayout.leftHeight - reviewLayout.metadataHeight)).toBeLessThanOrEqual(1);
  expect(reviewLayout.coverLeft).toBeGreaterThanOrEqual(reviewLayout.fieldsRight);
  expect(reviewLayout.coverWidth).toBeGreaterThanOrEqual(360);
  expect(Math.abs(reviewLayout.coverBottomGap)).toBeLessThanOrEqual(1);
  expect(reviewLayout.descriptionGap).toBeLessThanOrEqual(8);
  expect(reviewLayout.tagInsideFields).toBe(true);
  await expect(page.locator(".review-workflow-metadata").getByText("Hasheous 候选信息")).toHaveCount(0);
  await expect(page.locator(".review-workflow-metadata").getByText("信息来源", { exact: true })).toHaveCount(0);
  await page.screenshot({ path: evidencePath(testInfo, "review-workbench-3840-css-ultrawide.png"), fullPage: true });

  await page.getByRole("textbox", { name: "标题" }).fill("实时保存的标题");
  await expect(page.locator(".autosave-state")).toContainText(/等待保存|正在实时保存/);
  await expect(page.locator(".autosave-state")).toHaveText("已实时保存");
  await page.getByRole("link", { name: "返回待审核列表" }).click();
  await page.locator(`[data-review-item="${itemId(3)}"]`).getByRole("link", { name: "审核条目" }).click();
  await expect(page).toHaveURL(new RegExp(`/admin/reviews/${itemId(3)}`));
  await page.getByRole("textbox", { name: "标题" }).fill("Batch 1 Game 03 Saved");
  await expect(page.locator(".autosave-state")).toContainText(/等待保存|正在实时保存/);
  await expect(page.locator(".autosave-state")).toHaveText("已实时保存");

  await page.setViewportSize({ width: 1280, height: 800 });
  expect(await page.locator(".review-workflow-columns").evaluate((element) => getComputedStyle(element).gridTemplateColumns.split(" ").length)).toBe(1);
  const collapsedReviewLayout = await page.locator(".review-workflow-metadata").evaluate((element) => {
    const panel = element.getBoundingClientRect();
    const editor = element.querySelector<HTMLElement>(".review-workflow-editor")!.getBoundingClientRect();
    const publish = element.querySelector<HTMLElement>(".review-workflow-publish-layout")!.getBoundingClientRect();
    const fields = element.querySelector<HTMLElement>(".review-workflow-metadata-fields")!.getBoundingClientRect();
    const cover = element.querySelector<HTMLElement>(".review-workflow-cover-side")!.getBoundingClientRect();
    return { panelBottom: panel.bottom, editorBottom: editor.bottom, publishBottom: publish.bottom, fieldsBottom: fields.bottom, coverBottom: cover.bottom };
  });
  expect(collapsedReviewLayout.publishBottom).toBeLessThanOrEqual(collapsedReviewLayout.editorBottom);
  expect(collapsedReviewLayout.fieldsBottom).toBeLessThanOrEqual(collapsedReviewLayout.panelBottom);
  expect(collapsedReviewLayout.coverBottom).toBeLessThanOrEqual(collapsedReviewLayout.panelBottom);
  await page.screenshot({ path: evidencePath(testInfo, "review-detail-1280.png"), fullPage: true });
  await page.getByRole("button", { name: "通过并发布" }).click();
  const duplicateDialog = page.getByRole("alertdialog", { name: "仍然发布为新游戏？" });
  await expect(duplicateDialog).toBeVisible();
  await expect(duplicateDialog.getByRole("link")).toHaveCount(1);
  await duplicateDialog.getByRole("button", { name: "仍然发布为新游戏" }).click();
  await expect(page).not.toHaveURL(new RegExp(`/admin/reviews/${itemId(3)}(?:\\?|$)`));
  await expect(page.locator(".app-toast")).toContainText("游戏已成功发布");

  await page.getByRole("link", { name: "返回待审核列表" }).click();
  await expect(page).toHaveURL(new RegExp(`importJobId=${primaryJob}`));
  await expect(rows).toHaveCount(20);
  await page.goto(`/admin/reviews/${itemId(3)}?returnTo=${encodeURIComponent(`/admin/reviews?importJobId=${primaryJob}`)}`);
  await expect(page).toHaveURL(new RegExp(`/admin/reviews\\?importJobId=${primaryJob}$`));
  await expect(page.locator(".app-toast")).toContainText("审核条目已处理或不再可用");
  await expect(page.getByRole("heading", { name: "暂时无法读取数据" })).toHaveCount(0);
  await expect(page.locator(`[data-review-item="${itemId(3)}"]`)).toHaveCount(0);
  await page.getByRole("button", { name: "继续加载" }).click();
  await expect(rows).toHaveCount(40);
  await page.getByRole("button", { name: "继续加载" }).click();
  await expect(rows).toHaveCount(59);
  await page.locator(`[data-review-item="${itemId(58)}"]`).getByRole("link", { name: "审核条目" }).click();
  await page.getByRole("button", { name: "丢弃条目" }).click();
  await expect(page).not.toHaveURL(new RegExp(`/admin/reviews/${itemId(58)}(?:\\?|$)`));
  await expect(page.locator(".app-toast")).toContainText("条目已丢弃");

  const remaining: Array<{ itemId: string }> = [];
  let cursor: string | null = null;
  do {
    const query = new URLSearchParams({ importJobId: primaryJob, limit: "20" });
    if (cursor) {query.set("cursor", cursor);}
    const response = await page.request.get(`/api/v1/admin/reviews?${query}`, { maxRetries: 2 });
    expect(response.ok()).toBe(true);
    const pageResult = await response.json() as { items: Array<{ itemId: string }>; nextCursor: string | null };
    remaining.push(...pageResult.items);
    cursor = pageResult.nextCursor;
  } while (cursor);
  expect(remaining).toHaveLength(58);
  expect(remaining.some((item) => item.itemId === itemId(3) || item.itemId === itemId(58))).toBe(false);
});

test("ACC-UI-010 strict READY quick approval previews and publishes the complete filtered scope", async ({ page }, testInfo) => {
  const primaryJob = "20000000-0000-7000-8000-000000000001";
  await page.goto(`/admin/reviews?importJobId=${primaryJob}`);

  await page.getByRole("button", { name: "快速审批" }).click();
  const previewDialog = page.getByRole("alertdialog", { name: "快速审批可直接发布的游戏" });
  await expect(previewDialog).toBeVisible();
  await expect(previewDialog.getByText("可自动发布").locator("..")).toContainText("1");
  await expect(previewDialog.getByText("匹配待审核").locator("..")).toContainText("58");
  await previewDialog.getByRole("button", { name: "确认快速发布 1 个游戏" }).click();

  await expect(page).toHaveURL(/bulkApprovalId=[0-9a-f-]+/);
  const result = page.locator(".review-bulk-status");
  await expect(result.getByRole("heading", { name: "快速审批结果" })).toBeVisible({ timeout: 10_000 });
  await expect(result).toContainText("已处理 1 / 1");
  await expect(result.getByText("已发布").locator("..")).toContainText("1");
  const published = result.getByRole("link", { name: /实时保存的标题.*PUBLISHED/ });
  await expect(published).toHaveAttribute("href", /\/admin\/games\/[0-9a-f-]+/);
  await page.screenshot({ path: evidencePath(testInfo, "review-bulk-approval-result.png"), fullPage: true });
});

registerRuntimeAcceptanceTests();
