import { mkdirSync } from "node:fs";
import path from "node:path";
import { expect, test, type Page, type TestInfo } from "@playwright/test";

test.beforeEach(async ({ page }, testInfo) => {
  const multiViewport = /^ACC-UI-00[56]\b/.test(testInfo.title);
  test.skip(!multiViewport && testInfo.project.name !== "chrome-1280", "此状态型 Case 只消费一次共享验收夹具");
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000";
  const response = await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" }, headers: { Origin: origin }
  });
  expect(response.ok()).toBe(true);
});

function evidencePath(testInfo: TestInfo, name: string) {
  const caseDirectory = process.env.RETROM_ACCEPTANCE_CASE_DIR;
  if (!caseDirectory) return testInfo.outputPath(name);
  const screenshots = path.join(caseDirectory, "screenshots");
  mkdirSync(screenshots, { recursive: true });
  return path.join(screenshots, `${testInfo.project.name}-${name}`);
}

function pngDimensions(contents: Buffer) {
  expect(contents.subarray(1, 4).toString("ascii")).toBe("PNG");
  return { width: contents.readUInt32BE(16), height: contents.readUInt32BE(20) };
}

async function noPageOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
}

async function currentEmulatorBrightRatio(page: Page) {
  const playerFrame = page.frames().find((frame) => frame !== page.mainFrame());
  if (!playerFrame) return 0;
  return playerFrame.evaluate(async () => {
    const emulator = window.EJS_emulator;
    if (!emulator?.takeScreenshot) return 0;
    const result = await Promise.race([
      emulator.takeScreenshot("canvas", "png", 1),
      new Promise<null>((resolve) => setTimeout(() => resolve(null), 1_000)),
    ]);
    if (!result?.blob.size) return 0;
    const bitmap = await createImageBitmap(result.blob);
    const sample = document.createElement("canvas");
    sample.width = 64;
    sample.height = 64;
    const context = sample.getContext("2d", { alpha: false });
    if (!context) return 0;
    context.drawImage(bitmap, 0, 0, sample.width, sample.height);
    bitmap.close();
    const pixels = context.getImageData(0, 0, sample.width, sample.height).data;
    let brightPixels = 0;
    for (let index = 0; index < pixels.length; index += 4) {
      if ((pixels[index] + pixels[index + 1] + pixels[index + 2]) / 3 > 8) brightPixels += 1;
    }
    return brightPixels / (pixels.length / 4);
  });
}

type HorizontalGaps = { left: number; right: number };

async function pageCanvasGaps(page: Page, targetSelector = ".page-header"): Promise<HorizontalGaps> {
  const measurement = await page.evaluate((selector) => {
    const appBody = document.querySelector<HTMLElement>(".app-body");
    const content = document.querySelector<HTMLElement>(".content");
    const target = document.querySelector<HTMLElement>(selector);
    if (!appBody || !content || !target) return null;
    const appBodyRect = appBody.getBoundingClientRect();
    const contentRect = content.getBoundingClientRect();
    const targetRect = target.getBoundingClientRect();
    const contentStyle = getComputedStyle(content);
    const contentLeft = contentRect.left + Number.parseFloat(contentStyle.paddingLeft);
    const contentRight = contentRect.right - Number.parseFloat(contentStyle.paddingRight);
    return {
      canvasWidth: contentRight - contentLeft,
      targetLeftDelta: targetRect.left - contentLeft,
      targetRightDelta: contentRight - targetRect.right,
      left: targetRect.left - appBodyRect.left,
      right: appBodyRect.right - targetRect.right,
    };
  }, targetSelector);
  expect(measurement).not.toBeNull();
  expect(Math.abs(measurement!.targetLeftDelta)).toBeLessThanOrEqual(1);
  expect(Math.abs(measurement!.targetRightDelta)).toBeLessThanOrEqual(1);
  expect(measurement!.canvasWidth).toBeLessThanOrEqual(2321);
  return { left: measurement!.left, right: measurement!.right };
}

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
    if (index > 0) await expect(navigation.getByRole("link", { name: label, exact: true })).toHaveClass(/nav-child/);
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
  await page.addInitScript(() => {
    Object.defineProperty(document.documentElement, "requestFullscreen", { configurable: true, value: () => Promise.resolve() });
  });
  const routes = [
    ["/", ".home-page"], ["/library", ".page-layout-library"], ["/saves", ".page-layout-saves"],
    ["/favorites", ".favorite-page"], ["/recent", ".page-layout-recent"], ["/account", ".page-layout-detail"],
    ["/netplay", '.netplay-page:not([role="status"])'],
  ] as const;
  let sharedPageGaps: HorizontalGaps | null = null;
  for (const [route, selector] of routes) {
    await page.goto(route);
    const routeLayout = page.locator(selector);
    await expect(routeLayout).toBeVisible();
    await expect(routeLayout.locator(".page-header")).toBeVisible();
    await noPageOverflow(page);
    const gaps = await pageCanvasGaps(page, selector);
    if (sharedPageGaps) {
      expect(Math.abs(gaps.left - sharedPageGaps.left)).toBeLessThanOrEqual(1);
      expect(Math.abs(gaps.right - sharedPageGaps.right)).toBeLessThanOrEqual(1);
    } else {
      sharedPageGaps = gaps;
    }
  }
  await page.goto("/");
  await expect(page.locator("[data-home-layer]")).toHaveCount(5);
  await expect(page.getByText("我的资料库", { exact: true })).toBeVisible();
  if (testInfo.project.name === "chrome-4k-150") {
    const measureHomeLayout = () => page.evaluate(() => {
      const platform = document.querySelector<HTMLElement>('[data-home-layer="4"]');
      const platformTitle = platform?.querySelector<HTMLElement>("h2");
      const homePage = document.querySelector<HTMLElement>(".home-page");
      const appBody = document.querySelector<HTMLElement>(".app-body");
      const featuredMedia = document.querySelector<HTMLElement>(".home-featured-media")?.getBoundingClientRect();
      const featuredCover = document.querySelector<HTMLElement>(".home-featured-cover")?.getBoundingClientRect();
      const featuredCopy = document.querySelector<HTMLElement>(".home-featured-copy")?.getBoundingClientRect();
      const featuredSave = document.querySelector<HTMLElement>(".home-featured-save-preview")?.getBoundingClientRect();
      const featuredActions = document.querySelector<HTMLElement>(".home-featured-actions")?.getBoundingClientRect();
      return {
        fifthLayerBottom: document.querySelector<HTMLElement>('[data-home-layer="5"]')?.getBoundingClientRect().bottom ?? Number.POSITIVE_INFINITY,
        viewportHeight: document.documentElement.clientHeight,
        documentHeight: document.documentElement.scrollHeight,
        homeWidth: homePage?.getBoundingClientRect().width ?? 0,
        appBodyWidth: appBody?.getBoundingClientRect().width ?? Number.POSITIVE_INFINITY,
        platformTitleOffset: platform && platformTitle ? platformTitle.getBoundingClientRect().top - platform.getBoundingClientRect().top : Number.POSITIVE_INFINITY,
        featuredCoverHeightRatio: featuredMedia && featuredCover ? featuredCover.height / featuredMedia.height : 0,
        featuredCoverHeight: featuredCover?.height ?? 0,
        featuredHasSave: Boolean(featuredSave),
        featuredSaveWidthRatio: featuredMedia && featuredSave ? featuredSave.width / featuredMedia.width : 0,
        featuredSaveWidth: featuredSave?.width ?? 0,
        featuredActionsBottomGap: featuredMedia && featuredActions ? featuredMedia.bottom - featuredActions.bottom : Number.POSITIVE_INFINITY,
        featuredCoverActionsBottomDelta: featuredCover && featuredActions ? featuredCover.bottom - featuredActions.bottom : Number.POSITIVE_INFINITY,
        featuredCopyCoverGap: featuredCover && featuredCopy ? featuredCopy.left - featuredCover.right : Number.POSITIVE_INFINITY,
        featuredActionsCoverGap: featuredCover && featuredActions ? featuredActions.left - featuredCover.right : Number.POSITIVE_INFINITY,
      };
    });
    const homeLayout = await measureHomeLayout();
    expect(await page.evaluate(() => ({
      viewport: { width: innerWidth, height: innerHeight },
      screen: { width: window.screen.width, height: window.screen.height },
      devicePixelRatio: window.devicePixelRatio,
    }))).toEqual({
      viewport: { width: 2560, height: 1440 },
      screen: { width: 2560, height: 1440 },
      devicePixelRatio: 1.5,
    });
    const physical4KScreenshot = await page.screenshot({
      path: evidencePath(testInfo, "physical-4k-150-home.png"),
    });
    expect(pngDimensions(physical4KScreenshot)).toEqual({ width: 3840, height: 2160 });
    expect(homeLayout.fifthLayerBottom).toBeLessThanOrEqual(homeLayout.viewportHeight);
    expect(homeLayout.fifthLayerBottom).toBeGreaterThanOrEqual(homeLayout.viewportHeight - 48);
    expect(homeLayout.documentHeight).toBeLessThanOrEqual(homeLayout.viewportHeight);
    expect(homeLayout.homeWidth / homeLayout.appBodyWidth).toBeGreaterThanOrEqual(0.65);
    expect(homeLayout.platformTitleOffset).toBeLessThanOrEqual(20);
    expect(homeLayout.featuredCoverHeightRatio).toBeGreaterThanOrEqual(0.75);
    if (homeLayout.featuredHasSave) expect(homeLayout.featuredSaveWidthRatio).toBeGreaterThanOrEqual(0.24);
    expect(homeLayout.featuredActionsBottomGap).toBeLessThanOrEqual(50);
    expect(Math.abs(homeLayout.featuredCoverActionsBottomDelta)).toBeLessThanOrEqual(1);
    expect(homeLayout.featuredCopyCoverGap).toBeGreaterThanOrEqual(23);
    expect(homeLayout.featuredCopyCoverGap).toBeLessThanOrEqual(33);
    expect(Math.abs(homeLayout.featuredActionsCoverGap - homeLayout.featuredCopyCoverGap)).toBeLessThanOrEqual(1);

    const fluidFeaturedLayout: Array<{ coverHeight: number; saveWidth: number; copyGap: number }> = [];
    for (const width of [1900, 2200, 2500, 2800, 3100]) {
      await page.setViewportSize({ width, height: 1250 });
      const measurement = await measureHomeLayout();
      fluidFeaturedLayout.push({ coverHeight: measurement.featuredCoverHeight, saveWidth: measurement.featuredSaveWidth, copyGap: measurement.featuredCopyCoverGap });
      expect(Math.abs(measurement.featuredCoverActionsBottomDelta)).toBeLessThanOrEqual(1);
      expect(measurement.featuredCopyCoverGap).toBeGreaterThanOrEqual(23);
      expect(measurement.featuredCopyCoverGap).toBeLessThanOrEqual(33);
    }
    const fluidCopyGaps = fluidFeaturedLayout.map((measurement) => measurement.copyGap);
    expect(Math.max(...fluidCopyGaps) - Math.min(...fluidCopyGaps)).toBeLessThanOrEqual(9);
    for (let index = 1; index < fluidFeaturedLayout.length; index += 1) {
      expect(fluidFeaturedLayout[index].coverHeight).toBeGreaterThanOrEqual(fluidFeaturedLayout[index - 1].coverHeight - 1);
      expect(fluidFeaturedLayout[index].saveWidth).toBeGreaterThanOrEqual(fluidFeaturedLayout[index - 1].saveWidth - 1);
    }
    if (homeLayout.featuredHasSave) expect(fluidFeaturedLayout.at(-1)!.saveWidth).toBeGreaterThan(fluidFeaturedLayout[0].saveWidth);

    // Browser chrome or a non-maximized window can reduce the CSS viewport
    // further even on the same physical 4K display.
    await page.setViewportSize({ width: 1920, height: 950 });
    const scaledHomeLayout = await measureHomeLayout();
    expect(scaledHomeLayout.fifthLayerBottom).toBeLessThanOrEqual(scaledHomeLayout.viewportHeight);
    expect(scaledHomeLayout.documentHeight).toBeLessThanOrEqual(scaledHomeLayout.viewportHeight);
    expect(scaledHomeLayout.platformTitleOffset).toBeLessThanOrEqual(16);
    await page.setViewportSize({ width: 2560, height: 1440 });
  }
  await page.goto("/library");
  await expect(page.getByRole("heading", { name: "游戏库" })).toBeVisible();
  const launchableGame = page.locator(".library-game-card").filter({ hasText: "Sudoku" });
  await expect(launchableGame).toBeVisible();
  const libraryCard = await launchableGame.evaluate((card) => {
    const cardBox = card.getBoundingClientRect();
    const coverBox = card.querySelector(".library-game-cover")?.getBoundingClientRect();
    return { cardWidth: cardBox.width, coverRatio: coverBox ? coverBox.width / coverBox.height : 0 };
  });
  expect(libraryCard.cardWidth).toBeGreaterThanOrEqual(269);
  expect(libraryCard.cardWidth).toBeLessThanOrEqual(321);
  expect(Math.abs(libraryCard.coverRatio - 0.75)).toBeLessThanOrEqual(0.01);
  await launchableGame.getByRole("link").first().click();
  await expect(page.getByRole("button", { name: "开始游戏" })).toBeVisible();
  await noPageOverflow(page);
  const detailGaps = await pageCanvasGaps(page, ".game-detail-page");
  expect(Math.abs(detailGaps.left - sharedPageGaps!.left)).toBeLessThanOrEqual(1);
  expect(Math.abs(detailGaps.right - sharedPageGaps!.right)).toBeLessThanOrEqual(1);
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  const shell = page.locator(".player-shell");
  const stage = page.locator(".player-stage");
  await expect(shell).toBeVisible();
  const dimensions = await page.evaluate(() => ({ height: innerHeight, width: innerWidth }));
  const shellBox = await shell.boundingBox();
  const stageBox = await stage.boundingBox();
  expect(shellBox).toEqual({ x: 0, y: 0, width: dimensions.width, height: dimensions.height });
  expect(stageBox).toEqual({ x: 0, y: 0, width: dimensions.width, height: dimensions.height });
  await expect(page.locator(".player-loading")).toBeHidden({ timeout: 30_000 });
  const toolbar = page.locator(".player-toolbar");
  await expect(toolbar).toHaveCSS("opacity", "0", { timeout: 5_000 });
  const canvas = page.frameLocator(".player-frame").locator("canvas").first();
  await expect(canvas).toBeVisible();
  const canvasBox = await canvas.boundingBox();
  const drawingBuffer = await canvas.evaluate((element) => {
    const canvasElement = element as HTMLCanvasElement;
    return { height: canvasElement.height, width: canvasElement.width, runtimeAspect: window.EJS_emulator?.gameManager?.getVideoDimensions?.("aspect") ?? 0 };
  });
  expect(canvasBox).not.toBeNull();
  expect(canvasBox!.x).toBeGreaterThanOrEqual(-1);
  expect(canvasBox!.y).toBeGreaterThanOrEqual(-1);
  expect(canvasBox!.x + canvasBox!.width).toBeLessThanOrEqual(dimensions.width + 1);
  expect(canvasBox!.y + canvasBox!.height).toBeLessThanOrEqual(dimensions.height + 1);
  expect(Math.abs(canvasBox!.width / canvasBox!.height - (drawingBuffer.runtimeAspect || drawingBuffer.width / drawingBuffer.height))).toBeLessThanOrEqual(0.01);
  expect(Math.min(Math.abs(canvasBox!.width - dimensions.width), Math.abs(canvasBox!.height - dimensions.height))).toBeLessThanOrEqual(2);
  expect(Math.abs(canvasBox!.x - (dimensions.width - canvasBox!.width) / 2)).toBeLessThanOrEqual(2);
  expect(Math.abs(canvasBox!.y - (dimensions.height - canvasBox!.height) / 2)).toBeLessThanOrEqual(2);
  await page.mouse.move(dimensions.width / 2, dimensions.height / 2);
  await expect(toolbar).toHaveCSS("opacity", "0");
  await page.mouse.move(dimensions.width / 2, 1);
  await expect(toolbar).toHaveCSS("opacity", "1");
  await noPageOverflow(page);
  await page.screenshot({ path: evidencePath(testInfo, "user-layout.png"), fullPage: true });
});

test("ACC-UI-005 regression: sparse home rails keep game cards within desktop width caps", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator(".home-recent-rail")).toHaveCount(2);
  const layout = await page.locator(".home-recent-rail").evaluateAll((rails) => ({
    cardWidths: rails.flatMap((rail) =>
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
  await expect(page.getByRole("columnheader", { name: "启用状态" })).toBeVisible();
  const directoryRowHeights = await page.locator(".platform-directory-row").evaluateAll((rows) => rows.map((row) => row.getBoundingClientRect().height));
  expect(directoryRowHeights.length).toBeGreaterThan(0);
  expect(directoryRowHeights.every((height) => height >= 87 && height <= 90)).toBe(true);
  const populatedDirectory = page.locator(".platform-directory-row").filter({ hasText: /[1-9]\d* 款/ }).first();
  if (await populatedDirectory.count()) await expect(populatedDirectory.getByRole("checkbox", { name: /启用状态/ })).toBeEnabled();
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
  for (const control of await page.locator("main button:visible").all()) await expect(control).toHaveAccessibleName(/.+/);
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
    if (request.url().endsWith(`/api/v1/admin/reviews/${itemId(57)}/previews`)) reviewPreviewRequests.push(request.url());
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
    const cover = element.querySelector<HTMLElement>(".review-workflow-cover-side")!.getBoundingClientRect();
    const coverImage = element.querySelector<HTMLElement>(".review-workflow-cover-side .review-cover-upload")!.getBoundingClientRect();
    const coverStyle = getComputedStyle(element.querySelector<HTMLElement>(".review-workflow-cover-side")!);
    const description = element.querySelector<HTMLTextAreaElement>(".review-workflow-metadata-fields textarea")!;
    const descriptionLabel = description.closest("label")!;
    const descriptionText = document.createRange();
    descriptionText.selectNodeContents(descriptionLabel.firstChild!);
    const descriptionGap = description.getBoundingClientRect().top - descriptionText.getBoundingClientRect().bottom;
    return { leftHeight: left.height, metadataHeight: metadata.height, fieldsRight: fields.right, coverLeft: cover.left, coverWidth: cover.width, coverBottomGap: cover.bottom - coverImage.bottom - Number.parseFloat(coverStyle.paddingBottom), descriptionGap };
  });
  expect(Math.abs(reviewLayout.leftHeight - reviewLayout.metadataHeight)).toBeLessThanOrEqual(1);
  expect(reviewLayout.coverLeft).toBeGreaterThanOrEqual(reviewLayout.fieldsRight);
  expect(reviewLayout.coverWidth).toBeGreaterThanOrEqual(360);
  expect(Math.abs(reviewLayout.coverBottomGap)).toBeLessThanOrEqual(1);
  expect(reviewLayout.descriptionGap).toBeLessThanOrEqual(8);
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
    if (cursor) query.set("cursor", cursor);
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

test("ACC-RUN-002 one click requests fullscreen before launch and auto-starts the locked runtime", async ({ page }, testInfo) => {
  await page.addInitScript(() => {
    const record = (kind: string, value = "") => {
      const current = JSON.parse(sessionStorage.getItem("retrom:launch-events") ?? "[]") as Array<{ kind: string; value: string }>;
      current.push({ kind, value });
      sessionStorage.setItem("retrom:launch-events", JSON.stringify(current));
    };
    Object.defineProperty(Element.prototype, "requestFullscreen", { configurable: true, value: () => { record("fullscreen"); return Promise.resolve(); } });
    const originalFetch = window.fetch.bind(window);
    window.fetch = (input, init) => {
      record("fetch", typeof input === "string" ? input : input instanceof URL ? input.href : input.url);
      return originalFetch(input, init);
    };
  });
  const requests: string[] = [];
  page.on("request", (request) => requests.push(request.url()));
  await page.goto("/library");
  await page.locator(".library-game-card").filter({ hasText: "Sudoku" }).getByRole("link").first().click();
  const configResponse = page.waitForResponse((response) => /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  const configuration = await (await configResponse).json() as { emulatorGameId: number; gameName: string; gameTitle: string; coreName: string; platformName: string; gameUrl: string; loaderUrl: string; runtimePathOverrides: Record<string, string>; playerAdapterId: string; emulatorjsVersion: string };
  expect(Number.isSafeInteger(configuration.emulatorGameId) && configuration.emulatorGameId > 0).toBe(true);
  expect(configuration.gameName).toBe(`retrom-${configuration.emulatorGameId}`);
  expect(configuration.gameTitle).toBe("Sudoku");
  expect(configuration.coreName).toBe("mGBA");
  expect(configuration.platformName).toBe("Game Boy Advance");
  expect(configuration.playerAdapterId).toBe("ejs-4.2.3-v2");
  expect(configuration.emulatorjsVersion).toBe("4.2.3");
  expect(configuration.gameUrl).not.toMatch(/(?:blob:|file:|\/home\/)/);
  await expect(page.locator(".player-shell")).toBeVisible();
  await expect(page.getByRole("button", { name: "开始游戏" })).toHaveCount(0);
  await expect(page.locator(".player-loading")).toBeHidden({ timeout: 30_000 });
  const playerCanvas = page.frameLocator('iframe[title="Retrom EmulatorJS Player"]').locator("canvas.ejs_canvas");
  await expect(playerCanvas).toBeVisible({ timeout: 30_000 });
  await expect.poll(() => playerCanvas.evaluate((element) => {
    const runtimeWindow = element.ownerDocument.defaultView as Window & {
      EJS_emulator?: { allSettings?: Record<string, string> };
    };
    return {
      imageRendering: runtimeWindow.getComputedStyle(element).imageRendering,
      shader: runtimeWindow.EJS_emulator?.allSettings?.shader,
    };
  })).toEqual({ imageRendering: "pixelated", shader: "disabled" });
  await expect.poll(() => currentEmulatorBrightRatio(page), { timeout: 15_000, intervals: [500] }).toBeGreaterThan(0.02);
  await page.mouse.move(20, 20);
  const debugButton = page.getByRole("button", { name: "调试信息" });
  await expect(debugButton).toBeVisible();
  await debugButton.click();
  const debugPanel = page.getByRole("complementary", { name: "运行调试信息" });
  await expect(debugPanel).toBeVisible();
  await expect(debugPanel.getByText(/^\d+\.\d FPS$/)).toBeVisible({ timeout: 5_000 });
  await expect(debugPanel.getByText("mGBA", { exact: true })).toBeVisible();
  await expect(debugPanel.getByText("4.2.3", { exact: true })).toBeVisible();
  await expect(debugPanel.getByText("ejs-4.2.3-v2", { exact: true })).toBeVisible();
  await expect(debugPanel.getByText("运行中", { exact: true })).toBeVisible();
  await expect(page.locator(".player-pause-overlay")).not.toHaveClass(/is-visible/);
  await page.screenshot({ path: evidencePath(testInfo, "player-debug.png"), fullPage: true });
  await debugPanel.getByRole("button", { name: "关闭调试信息面板" }).click();
  await expect(page.locator("#player-debug-panel")).toHaveAttribute("aria-hidden", "true");
  await page.mouse.move(20, 20);
  await page.getByRole("button", { name: "更多操作" }).click();
  await expect(page.getByRole("menuitem", { name: /创建存档/ })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "创建存档", exact: true })).toBeVisible();
  await page.getByRole("menuitem", { name: "模拟器设置" }).click();
  const renderingToolbar = page.getByRole("region", { name: "模拟器设置工具栏" });
  const renderingMode = renderingToolbar.getByRole("combobox", { name: "画面模式" });
  await expect(renderingMode).toHaveValue("pixel");
  await renderingMode.selectOption("clear");
  await expect.poll(() => playerCanvas.evaluate((element) => {
    const runtimeWindow = element.ownerDocument.defaultView as Window & {
      EJS_emulator?: { allSettings?: Record<string, string> };
    };
    return runtimeWindow.EJS_emulator?.allSettings?.shader;
  })).toBe("retrom-sharp-bilinear");
  await renderingMode.selectOption("sharpen");
  await expect.poll(() => playerCanvas.evaluate((element) => {
    const runtimeWindow = element.ownerDocument.defaultView as Window & {
      EJS_emulator?: { allSettings?: Record<string, string> };
    };
    return runtimeWindow.EJS_emulator?.allSettings?.shader;
  })).toBe("retrom-adaptive-sharpen");
  await renderingToolbar.getByRole("button", { name: "收起" }).click();
  await playerCanvas.click({ position: { x: 100, y: 100 } });
  await expect.poll(() => currentEmulatorBrightRatio(page), { timeout: 15_000, intervals: [500] }).toBeGreaterThan(0.02);
  await page.mouse.move(20, 20);
  await page.getByRole("button", { name: "更多操作" }).click();
  await page.getByRole("menuitem", { name: "模拟器设置" }).click();
  await renderingMode.selectOption("original");
  await expect.poll(() => playerCanvas.evaluate((element) => getComputedStyle(element).imageRendering)).toBe("auto");
  await renderingMode.selectOption("pixel");
  await expect.poll(() => playerCanvas.evaluate((element) => {
    const runtimeWindow = element.ownerDocument.defaultView as Window & {
      EJS_emulator?: { allSettings?: Record<string, string> };
    };
    return runtimeWindow.EJS_emulator?.allSettings?.shader;
  })).toBe("disabled");
  const events = await page.evaluate(() => JSON.parse(sessionStorage.getItem("retrom:launch-events") ?? "[]") as Array<{ kind: string; value: string }>);
  const fullscreenIndex = events.findIndex((event) => event.kind === "fullscreen");
  const launchIndex = events.findIndex((event) => event.kind === "fetch" && event.value === "/api/v1/launches");
  expect(fullscreenIndex).toBeGreaterThanOrEqual(0);
  expect(launchIndex).toBeGreaterThan(fullscreenIndex);
  expect(requests.some((url) => url.endsWith(configuration.loaderUrl))).toBe(true);
  for (const artifactURL of Object.values(configuration.runtimePathOverrides)) expect(requests.some((url) => url.endsWith(artifactURL))).toBe(true);
  expect(requests.some((url) => /\/localization\/zh-CN\.json$/.test(url))).toBe(true);
  const applicationHost = new URL(page.url()).host;
  expect(requests.some((url) => {
    try {
      const parsed = new URL(url);
      if (parsed.protocol === "about:") return false;
      if (parsed.protocol === "blob:") return new URL(url.slice("blob:".length)).host !== applicationHost;
      return parsed.host !== applicationHost;
    } catch { return false; }
  })).toBe(false);
  await page.screenshot({ path: evidencePath(testInfo, "one-click-auto-start.png"), fullPage: true });
});

test("ACC-RUN-003 fullscreen refusal and launch deep-link credentials remain recoverable", async ({ page, browser }, testInfo) => {
  await page.addInitScript(() => {
    Object.defineProperty(Element.prototype, "requestFullscreen", { configurable: true, value: () => Promise.reject(new DOMException("denied", "NotAllowedError")) });
  });
  await page.goto("/library");
  await page.locator(".library-game-card").filter({ hasText: "Sudoku" }).getByRole("link").first().click();
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  const playURL = page.url();
  await expect(page.getByRole("button", { name: "全屏" })).toBeVisible();
  await expect(page.getByRole("button", { name: "开始游戏" })).toHaveCount(0);
  await page.reload();
  await expect(page.locator(".player-shell")).toBeVisible();
  await expect(page.getByRole("button", { name: "全屏" })).toBeVisible();
  await expect(page.getByRole("button", { name: "开始游戏" })).toHaveCount(0);
  const freshContext = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const freshPage = await freshContext.newPage();
  await freshPage.goto(playURL);
  await expect(freshPage.getByText("启动会话不可用，请从游戏详情或存档重新开始。", { exact: true })).toBeVisible();
  await expect(freshPage.getByRole("link", { name: "返回游戏库" })).toBeVisible();
  await freshContext.close();
  await page.screenshot({ path: evidencePath(testInfo, "fullscreen-refused-recovery.png"), fullPage: true });
});

test("ACC-RUN-004 BIOS blockers stop launch while hash warnings auto-start", async ({ page }, testInfo) => {
  await page.addInitScript(() => {
    const record = (event: string) => sessionStorage.setItem(`retrom:${event}`, "true");
    Object.defineProperty(Element.prototype, "requestFullscreen", { configurable: true, value: () => { record("fullscreen-requested"); return Promise.resolve(); } });
    Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => document.documentElement });
    Object.defineProperty(document, "exitFullscreen", { configurable: true, value: () => { record("fullscreen-exited"); return Promise.resolve(); } });
  });
  await page.goto("/admin/bios?scope=FULL_CATALOG");
  const gbaRow = page.getByRole("row").filter({ hasText: "gba_bios.bin" });
  await gbaRow.locator('input[type="file"]').setInputFiles({ name: "gba_bios.bin", mimeType: "application/octet-stream", buffer: Buffer.from("retrom-invalid-bios\n") });
  await expect(gbaRow.getByText("校验值不一致", { exact: true })).toBeVisible();
  await page.goto("/library");
  await page.locator(".library-game-card").filter({ hasText: "Sudoku" }).getByRole("link").first().click();
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  await expect(page.locator(".player-loading")).toBeHidden({ timeout: 30_000 });
  await expect(page.getByRole("button", { name: "开始游戏" })).toHaveCount(0);
  await page.mouse.move(20, 20);
  await page.getByRole("button", { name: "查看运行提醒" }).click();
  await expect(page.getByText("BIOS 校验值与目录期望不同，但当前允许运行。", { exact: true })).toBeVisible();
  await page.screenshot({ path: evidencePath(testInfo, "bios-hash-warning-autostart.png"), fullPage: true });
  await page.getByRole("button", { name: "返回并退出游戏" }).click();
  const exitDialog = page.getByRole("alertdialog", { name: "退出游戏？" });
  await expect(exitDialog).toBeVisible();
  await exitDialog.getByRole("button", { name: "退出游戏", exact: true }).click();
  await expect(page).toHaveURL(/\/games\/[0-9a-f-]+$/);
  await page.goBack();
  await expect(page).toHaveURL(/\/library$/);
  await expect(page.locator(".player-shell")).toHaveCount(0);

  await page.goto("/library");
  await page.locator(".library-game-card").filter({ hasText: "Acceptance Missing FDS BIOS" }).getByRole("link").first().click();
  await expect(page).toHaveURL(/\/games\/60000000-0000-7000-8000-000000000001$/);
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page.locator(".launch-panel [role=alert]")).toContainText("LAUNCH_BIOS_MISSING", { timeout: 30_000 });
  await expect(page.getByRole("link", { name: "前往 BIOS 管理" })).toBeVisible();
  await expect(page).toHaveURL(/\/games\/60000000-0000-7000-8000-000000000001$/);
  expect(await page.evaluate(() => sessionStorage.getItem("retrom:fullscreen-requested"))).toBe("true");
  expect(await page.evaluate(() => sessionStorage.getItem("retrom:fullscreen-exited"))).toBe("true");
  await page.screenshot({ path: evidencePath(testInfo, "required-bios-blocker-repair.png"), fullPage: true });
});

type PublicArcadeSmokeExpectation = {
  platformInstanceId: string;
  core: string;
  coreName: string;
  runtimePathOverrides: Record<string, string>;
  screenshotName: string;
  schemaV2Title: string;
  schemaV2ScreenshotName: string;
};

async function verifyPublicArcadeSmoke(
  page: Page,
  testInfo: TestInfo,
  expectation: PublicArcadeSmokeExpectation,
) {
  await page.addInitScript(() => {
    Object.defineProperty(Element.prototype, "requestFullscreen", {
      configurable: true,
      value: () => Promise.resolve(),
    });
  });
  const runtimeRequests: string[] = [];
  const runtimeFailures: string[] = [];
  const pageErrors: string[] = [];
  page.on("request", (request) => {
    if (new URL(request.url()).pathname.startsWith("/runtime/")) runtimeRequests.push(request.url());
  });
  page.on("requestfailed", (request) => {
    const pathname = new URL(request.url()).pathname;
    const errorText = request.failure()?.errorText ?? "unknown";
    const canceledPlayerProbe = errorText === "net::ERR_ABORTED"
      && /\/runtime\/launches\/[^/]+\/(config|persistent-save)$/.test(pathname);
    if (pathname.startsWith("/runtime/") && !canceledPlayerProbe) {
      runtimeFailures.push(`${request.url()}: ${errorText}`);
    }
  });
  page.on("pageerror", (error) => pageErrors.push(error.message));

  await page.goto(`/library?platformInstanceId=${expectation.platformInstanceId}&q=pacman`);
  const game = page.locator(".library-game-card").filter({ hasText: "pacman" });
  await expect(game).toHaveCount(1);
  await game.getByRole("link").first().click();
  const configResponse = page.waitForResponse((response) =>
    /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  const configuration = await (await configResponse).json() as {
    core: string;
    runtimeCore: string;
    coreName: string;
    emulatorjsVersion: string;
    playerAdapterId: string;
    gameUrl: string;
    parentUrl: string | null;
    biosUrl: string | null;
    runtimePathOverrides: Record<string, string>;
  };
  expect(configuration).toMatchObject({
    core: expectation.core,
    runtimeCore: expectation.core,
    coreName: expectation.coreName,
    emulatorjsVersion: "4.2.3",
    playerAdapterId: "ejs-4.2.3-v2",
  });
  expect(configuration.gameUrl).toMatch(/\/game\/pacman\.zip$/);
  expect(configuration.parentUrl).toMatch(/\/parent\/bundle\.zip$/);
  expect(configuration.biosUrl).toMatch(/\/bios\/bundle\.zip$/);
  expect(configuration.runtimePathOverrides).toEqual(expectation.runtimePathOverrides);

  await expect(page.locator(".player-loading")).toBeHidden({ timeout: 60_000 });
  const canvas = page.frameLocator('iframe[title="Retrom EmulatorJS Player"]').locator("canvas");
  await expect(canvas).toBeVisible({ timeout: 10_000 });
  await expect.poll(() => canvas.evaluate((element) => {
    const runtimeWindow = element.ownerDocument.defaultView as Window & {
      EJS_emulator?: { allSettings?: Record<string, string> };
    };
    return {
      imageRendering: runtimeWindow.getComputedStyle(element).imageRendering,
      shader: runtimeWindow.EJS_emulator?.allSettings?.shader,
    };
  })).toEqual({ imageRendering: "pixelated", shader: "disabled" });
  const canvasSize = await canvas.evaluate((element) => {
    if (!(element instanceof HTMLCanvasElement)) return { width: 0, height: 0 };
    return { width: element.width, height: element.height };
  });
  expect(canvasSize.width).toBeGreaterThan(0);
  expect(canvasSize.height).toBeGreaterThan(0);
  const firstFrame = await canvas.screenshot();
  await page.waitForTimeout(1_200);
  const secondFrame = await canvas.screenshot();
  expect(firstFrame.equals(secondFrame)).toBe(false);

  await page.mouse.move(20, 20);
  await page.getByRole("button", { name: "调试信息" }).click();
  const debugPanel = page.getByRole("complementary", { name: "运行调试信息" });
  await expect(debugPanel.getByText(expectation.coreName, { exact: true })).toBeVisible();
  await expect(debugPanel.getByText("运行中", { exact: true })).toBeVisible();
  const fps = debugPanel.getByText(/^\d+\.\d FPS$/);
  await expect(fps).toBeVisible({ timeout: 5_000 });
  expect(Number.parseFloat(await fps.innerText())).toBeGreaterThan(0);
  for (const url of [configuration.gameUrl, configuration.parentUrl!, configuration.biosUrl!]) {
    expect(runtimeRequests.some((requestURL) => requestURL.endsWith(url))).toBe(true);
  }
  expect(runtimeFailures).toEqual([]);
  expect(pageErrors).toEqual([]);
  await page.screenshot({ path: evidencePath(testInfo, expectation.screenshotName), fullPage: true });
}

async function verifyPersistedArcadeSchemaV2Launch(
  page: Page,
  testInfo: TestInfo,
  expectation: PublicArcadeSmokeExpectation,
) {
  await page.goto(`/library?platformInstanceId=${expectation.platformInstanceId}&q=${encodeURIComponent(expectation.schemaV2Title)}`);
  const game = page.locator(".library-game-card").filter({ hasText: expectation.schemaV2Title });
  await expect(game).toHaveCount(1);
  await game.getByRole("link").first().click();
  await expect(page).toHaveURL(/\/games\/[0-9a-f-]+$/);
  const gameId = page.url().split("/").at(-1)!;
  const [detailResponse, adminResponse] = await Promise.all([
    page.request.get(`/api/v1/games/${gameId}`),
    page.request.get(`/api/v1/admin/games/${gameId}`),
  ]);
  expect(detailResponse.ok()).toBe(true);
  expect(adminResponse.ok()).toBe(true);
  const detail = await detailResponse.json() as {
    coreOptions: Array<{ coreId: string; status: string; datVersionId: string | null }>;
  };
  const coreOption = detail.coreOptions.find((option) => option.coreId === expectation.core);
  expect(coreOption).toMatchObject({ status: "READY" });
  expect(coreOption?.datVersionId).toBeTruthy();
  const admin = await adminResponse.json() as {
    variants: Array<{
      coreId: string;
      currentRevisionId: string;
      revisions: Array<{
        id: string;
        datVersionId: string | null;
        dependencySnapshot: {
          schemaVersion: number;
          datVersionId?: string;
          bios?: unknown;
          dependencies?: Array<{ kind: string; machine: string; state: string }>;
        };
      }>;
    }>;
  };
  const variant = admin.variants.find((item) => item.coreId === expectation.core);
  const revision = variant?.revisions.find((item) => item.id === variant.currentRevisionId);
  expect(revision?.datVersionId).toBe(coreOption?.datVersionId);
  expect(revision?.dependencySnapshot).toMatchObject({
    schemaVersion: 2,
    datVersionId: coreOption?.datVersionId,
    dependencies: expect.arrayContaining([
      expect.objectContaining({ kind: "PARENT", machine: "puckman", state: "SATISFIED_EXTERNAL" }),
      expect.objectContaining({ kind: "BIOS_OR_BASE", machine: "retrombios", state: "SATISFIED_EXTERNAL" }),
    ]),
  });
  expect(revision?.dependencySnapshot.bios).toBeUndefined();

  const configResponse = page.waitForResponse((response) =>
    /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  const configuration = await (await configResponse).json() as {
    runtimeCore: string;
    parentUrl: string | null;
    biosUrl: string | null;
    warnings: string[];
  };
  expect(configuration.runtimeCore).toBe(expectation.core);
  expect(configuration.parentUrl).toMatch(/\/parent\/bundle\.zip$/);
  expect(configuration.biosUrl).toMatch(/\/bios\/bundle\.zip$/);
  expect(configuration.warnings).toContain("REVIEW_SCREENSHOT_OVERRIDE");
  await expect(page.locator(".player-loading")).toBeHidden({ timeout: 60_000 });
  await expect(page.frameLocator('iframe[title="Retrom EmulatorJS Player"]').locator("canvas")).toBeVisible({ timeout: 10_000 });
  await page.screenshot({ path: evidencePath(testInfo, expectation.schemaV2ScreenshotName), fullPage: true });
}

test("ACC-RUN-006 public MAME 2003 split set locks test-only built-in DAT, Parent and BIOS, then executes frames", async ({ page }, testInfo) => {
  test.setTimeout(150_000);
  const expectation: PublicArcadeSmokeExpectation = {
    platformInstanceId: "01980000-0000-7000-8000-000000000008",
    core: "mame2003",
    coreName: "MAME 2003",
    runtimePathOverrides: {
      "mame2003-wasm.data": "/runtime/emulatorjs/4.2.3/overrides/mame2003-4.2.1-wasm.data",
    },
    screenshotName: "mame2003-public-smoke-running.png",
    schemaV2Title: "MAME 2003 Schema V2 Regression",
    schemaV2ScreenshotName: "mame2003-schema-v2-direct-launch.png",
  };
  await verifyPublicArcadeSmoke(page, testInfo, expectation);
  await verifyPersistedArcadeSchemaV2Launch(page, testInfo, expectation);
});

test("ACC-RUN-007 public FBNeo split set locks test-only built-in DAT, Parent and BIOS, then executes frames", async ({ page }, testInfo) => {
  test.setTimeout(150_000);
  const expectation: PublicArcadeSmokeExpectation = {
    platformInstanceId: "01980000-0000-7000-8000-000000000006",
    core: "fbneo",
    coreName: "FinalBurn Neo",
    runtimePathOverrides: {
      "fbneo-wasm.data": "/runtime/emulatorjs/4.2.3/data/cores/fbneo-wasm.data",
    },
    screenshotName: "fbneo-public-smoke-running.png",
    schemaV2Title: "FBNeo Schema V2 Regression",
    schemaV2ScreenshotName: "fbneo-schema-v2-direct-launch.png",
  };
  await verifyPublicArcadeSmoke(page, testInfo, expectation);
  await verifyPersistedArcadeSchemaV2Launch(page, testInfo, expectation);
});

test("ACC-SAVE-002 detail, saves, and home resume the locked save in one click", async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  await page.addInitScript(() => {
    Object.defineProperty(Element.prototype, "requestFullscreen", { configurable: true, value: () => Promise.resolve() });
  });
  await page.goto("/library");
  await page.locator(".library-game-card").filter({ hasText: "Sudoku" }).getByRole("link").first().click();
  await expect(page).toHaveURL(/\/games\/[0-9a-f-]+$/);
  const detailURL = page.url();
  const gameId = detailURL.split("/").at(-1)!;
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  await expect(page.locator(".player-shell")).toBeVisible();
  await expect(page.locator(".player-loading")).toBeHidden({ timeout: 60_000 });
  const launchId = page.url().split("/").at(-1)!;
  const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64");
  const saveResponse = await page.request.post(`/runtime/launches/${launchId}/save-states`, {
    headers: { Origin: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000", "Idempotency-Key": crypto.randomUUID() },
    multipart: {
      metadata: { name: "metadata.json", mimeType: "application/json", buffer: Buffer.from('{"name":"Acceptance Save"}') },
      state: { name: "state.bin", mimeType: "application/octet-stream", buffer: Buffer.from([1, 2, 3, 4]) },
      screenshot: { name: "screenshot.png", mimeType: "image/png", buffer: png },
    },
  });
  expect(saveResponse.status()).toBe(201);
  const saved = await saveResponse.json() as { saveStateId: string };

  await page.goto(detailURL);
  await page.getByRole("button", { name: "从存档继续" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  await page.goto("/saves");
  await expect(page.getByRole("heading", { name: "最近保存" })).toBeVisible();
  await expect(page.getByRole("region", { name: "筛选存档" })).toBeVisible();
  await expect(page.locator(".save-library-group").filter({ hasText: "Sudoku" })).toBeVisible();
  const saveFilterHeight = await page.locator(".save-library-toolbar").evaluate((element) => element.getBoundingClientRect().height);
  await page.getByPlaceholder("搜索游戏或存档名称").fill("Sudoku");
  await expect(page.locator(".save-library-toolbar")).toContainText("当前显示 1 份");
  expect(await page.locator(".save-library-toolbar").evaluate((element) => element.getBoundingClientRect().height)).toBe(saveFilterHeight);
  await noPageOverflow(page);
  const resumedConfigResponse = page.waitForResponse((response) =>
    /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
  await page.getByRole("button", { name: "从这里继续" }).first().click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  await resumedConfigResponse;
  await expect(page.locator(".player-loading")).toBeHidden({ timeout: 60_000 });
  const latestLaunchId = page.url().split("/").at(-1)!;
  const latestSaveResponse = await page.request.post(`/runtime/launches/${latestLaunchId}/save-states`, {
    headers: { Origin: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000", "Idempotency-Key": crypto.randomUUID() },
    multipart: {
      metadata: { name: "metadata.json", mimeType: "application/json", buffer: Buffer.from('{"name":"Latest Session Save"}') },
      state: { name: "state.bin", mimeType: "application/octet-stream", buffer: Buffer.from([5, 6, 7, 8]) },
      screenshot: { name: "screenshot.png", mimeType: "image/png", buffer: png },
    },
  });
  expect(latestSaveResponse.status()).toBe(201);
  await page.goto("/recent");
  await expect(page.getByRole("region", { name: "游玩统计" })).toBeVisible();
  await expect(page.getByRole("region", { name: "筛选最近游玩" })).toBeVisible();
  await expect(page.locator(".recent-history-row").filter({ hasText: "Sudoku" })).toBeVisible();
  const filterHeight = await page.locator(".recent-filter-panel").evaluate((element) => element.getBoundingClientRect().height);
  await page.getByRole("searchbox", { name: "搜索游戏" }).fill("Sudoku");
  await expect(page.locator(".recent-result-count")).toContainText("1");
  expect(await page.locator(".recent-filter-panel").evaluate((element) => element.getBoundingClientRect().height)).toBe(filterHeight);
  await noPageOverflow(page);
  await page.goto("/");
  await page.getByRole("button", { name: "继续游玩" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);

  const authContext = await page.request.get("/api/v1/auth/context");
  const csrfToken = (await authContext.json() as { csrfToken: string }).csrfToken;
  const mismatch = await page.request.post("/api/v1/launches", {
    headers: { Origin: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000", "X-Retrom-Csrf": csrfToken, "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() },
    data: { gameId, coreId: "gambatte", saveStateId: saved.saveStateId, dosEntry: null, returnTo: "/saves", clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true } },
  });
  expect(mismatch.status()).toBe(422);
  expect((await mismatch.json() as { error: { code: string } }).error.code).toBe("LAUNCH_BLOCKED");
  await page.screenshot({ path: evidencePath(testInfo, "three-save-resume-entry-points.png"), fullPage: true });
});

test("LAN HTTP upload works without secure-context crypto APIs", async ({ page }) => {
  await page.addInitScript(() => {
    const cryptoPrototype = Object.getPrototypeOf(window.crypto) as object;
    Object.defineProperty(cryptoPrototype, "randomUUID", { configurable: true, value: undefined });
    Object.defineProperty(cryptoPrototype, "subtle", { configurable: true, value: undefined });
  });
  await page.goto("/admin/imports/new");
  expect(await page.evaluate(() => ({ randomUUID: typeof crypto.randomUUID, subtle: typeof crypto.subtle }))).toEqual({
    randomUUID: "undefined",
    subtle: "undefined",
  });
  const fileChooserPromise = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: "选择文件" }).click();
  const fileChooser = await fileChooserPromise;
  await fileChooser.setFiles({
    name: "lan-http-regression.nes",
    mimeType: "application/octet-stream",
    buffer: Buffer.from([0x4e, 0x45, 0x53, 0x1a, 0, 0, 0, 0]),
  });
  await expect(page.getByRole("heading", { name: /^1 个文件/ })).toBeVisible();
  await page.getByRole("button", { name: "下一步" }).click();
  await expect(page.locator("#directory")).toHaveValue("");
  const targetDirectory = await page.locator("#directory option:not([disabled])").first().getAttribute("value");
  expect(targetDirectory).toBeTruthy();
  await page.locator("#directory").selectOption(targetDirectory!);
  await page.locator("#provider").selectOption("NONE");
  await page.getByRole("button", { name: "开始上传并验证" }).click();
  await expect(page.getByRole("heading", { name: "导入任务已创建" })).toBeVisible({ timeout: 30_000 });
  await page.getByRole("button", { name: "查看任务进度 →" }).click();
  await expect(page).toHaveURL(/\/admin\/imports\/tasks$/);
});
