import { mkdirSync } from "node:fs";
import path from "node:path";
import { expect, test, type Page, type TestInfo } from "@playwright/test";

test.beforeEach(({}, testInfo) => {
  const multiViewport = /^ACC-UI-00[56]\b/.test(testInfo.title);
  test.skip(!multiViewport && testInfo.project.name !== "chrome-1280", "此状态型 Case 只消费一次共享验收夹具");
});

function evidencePath(testInfo: TestInfo, name: string) {
  const caseDirectory = process.env.RETROM_ACCEPTANCE_CASE_DIR;
  if (!caseDirectory) return testInfo.outputPath(name);
  const screenshots = path.join(caseDirectory, "screenshots");
  mkdirSync(screenshots, { recursive: true });
  return path.join(screenshots, `${testInfo.project.name}-${name}`);
}

async function noPageOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
}

test("ACC-UI-001 user navigation and account-free access", async ({ page }, testInfo) => {
  await page.goto("/");
  const navigation = page.getByRole("navigation", { name: "主要导航" });
  await expect(navigation.getByRole("link")).toHaveCount(3);
  for (const label of ["首页", "游戏库", "我的存档"]) await expect(navigation.getByRole("link", { name: label })).toBeVisible();
  await navigation.getByRole("link", { name: "游戏库" }).click();
  await expect(page).toHaveURL(/\/library$/);
  const firstGame = page.locator(".game-card").first();
  if (await firstGame.count()) {
    await firstGame.click();
    await expect(page).toHaveURL(/\/games\/[0-9a-f-]+$/);
    await expect(page.getByRole("navigation", { name: "主要导航" }).getByRole("link")).toHaveCount(3);
  }
  await page.getByRole("link", { name: "管理后台" }).click();
  await expect(page).toHaveURL(/\/admin\/imports$/);
  await expect(page.getByRole("link", { name: "返回用户侧" })).toBeVisible();
  await page.screenshot({ path: evidencePath(testInfo, "user-navigation.png"), fullPage: true });
});

test("ACC-UI-002 import parent and child routes preserve browser history", async ({ page }, testInfo) => {
  const routes = [
    ["/admin/imports", "游戏入库"],
    ["/admin/imports/new", "导入文件 / 目录"],
    ["/admin/imports/tasks", "任务进度"],
    ["/admin/reviews", "待审核"],
    ["/admin/reviews/history", "审核历史"],
  ] as const;
  for (const [index, [route, label]] of routes.entries()) {
    await page.goto(route);
    const navigation = page.getByRole("navigation", { name: "主要导航" });
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
  await expect(page.getByText("有效游玩时长")).toBeVisible();
  await page.goto("/library");
  await page.getByRole("textbox", { name: "搜索" }).fill("Sudoku");
  await page.getByRole("combobox", { name: "游戏平台" }).selectOption("gba");
  const directory = page.getByRole("combobox", { name: "游戏目录" });
  await expect(directory).toBeVisible();
  const directoryValue = await directory.locator("option").nth(1).getAttribute("value");
  expect(directoryValue).toBeTruthy();
  await directory.selectOption(directoryValue!);
  await page.getByRole("button", { name: "查看结果" }).click();
  await expect(page).toHaveURL(/q=Sudoku/);
  await expect(page).toHaveURL(/platformId=gba/);
  await expect(page).toHaveURL(/platformInstanceId=/);
  await page.reload();
  await expect(page.getByRole("textbox", { name: "搜索" })).toHaveValue("Sudoku");
  await expect(page.getByRole("combobox", { name: "游戏平台" })).toHaveValue("gba");
  await expect(page.getByRole("link", { name: /移除关键词：Sudoku/ })).toBeVisible();
  const game = page.locator(".game-card").first();
  await expect(game).toBeVisible();
  await game.click();
  await expect(page).toHaveURL(/\/games\/[0-9a-f-]+$/);
  await expect(page.getByRole("button", { name: "开始游戏" })).toBeVisible();
  await expect(page.getByText("推荐配置", { exact: true })).toBeVisible();
  const heroHeight = await page.locator(".hero").evaluate((element) => element.getBoundingClientRect().height);
  await page.getByText("更换运行方式", { exact: true }).click();
  await expect(page.getByRole("combobox", { name: "运行引擎" }).locator("option:checked")).toContainText("推荐");
  expect(await page.locator(".hero").evaluate((element) => element.getBoundingClientRect().height)).toBe(heroHeight);
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
  await expect(page.getByRole("heading", { name: "BIOS 管理" })).toBeVisible();
  const gbaRow = page.getByRole("row").filter({ hasText: "gba_bios.bin" });
  await gbaRow.locator('input[type="file"]').setInputFiles({ name: "gba_bios.bin", mimeType: "application/octet-stream", buffer: Buffer.from("retrom-invalid-bios\n") });
  await expect(gbaRow.getByText("校验值不一致", { exact: true })).toBeVisible();
  await expect(gbaRow.getByText("MD5", { exact: false })).toBeVisible();
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
  const routes = ["/", "/library", "/saves"];
  for (const route of routes) {
    await page.goto(route);
    await expect(page.locator(".page-header")).toBeVisible();
    await noPageOverflow(page);
  }
  await page.goto("/");
  await expect(page.getByText("有效游玩时长", { exact: true })).toBeVisible();
  const metricAlignment = await page.locator(".home-metrics .kpi").evaluateAll((cards) => cards.map((card) => {
    const label = card.querySelector(".kpi-label")?.getBoundingClientRect();
    const accent = card.querySelector(".kpi-accent")?.getBoundingClientRect();
    const value = card.querySelector(".kpi-value strong")?.getBoundingClientRect();
    return label && accent && value ? {
      accentToLabel: Math.abs((accent.top + accent.height / 2) - (label.top + label.height / 2)),
      accentToValue: Math.abs((accent.top + accent.height / 2) - (value.top + value.height / 2)),
      accentAfterValue: accent.left > value.right,
    } : null;
  }));
  expect(metricAlignment).toHaveLength(3);
  expect(metricAlignment.every((item) => item !== null && item.accentToLabel <= 1 && item.accentToValue <= 1 && item.accentAfterValue), JSON.stringify(metricAlignment)).toBe(true);
  await page.goto("/library");
  await expect(page.getByRole("heading", { name: "游戏库" })).toBeVisible();
  const launchableGame = page.locator(".game-card").filter({ hasText: "Sudoku" });
  await expect(launchableGame).toBeVisible();
  const expectedColumns = testInfo.project.name === "chrome-1280" ? 4 : testInfo.project.name === "chrome-1440p" ? 6 : 8;
  expect(await page.locator(".game-grid").evaluate((element) => getComputedStyle(element).gridTemplateColumns.split(" ").length)).toBe(expectedColumns);
  await launchableGame.click();
  await expect(page.getByRole("button", { name: "开始游戏" })).toBeVisible();
  await noPageOverflow(page);
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
  await page.mouse.move(dimensions.width / 2, dimensions.height / 2);
  await expect(toolbar).toHaveCSS("opacity", "1");
  await noPageOverflow(page);
  await page.screenshot({ path: evidencePath(testInfo, "user-layout.png"), fullPage: true });
});

test("ACC-UI-006 admin pages remain reachable at desktop breakpoints", async ({ page }, testInfo) => {
  const routes = [
    "/admin/imports", "/admin/imports/new", "/admin/imports/tasks", "/admin/reviews",
    "/admin/reviews/history", "/admin/games", "/admin/platform-instances", "/admin/bios", "/admin/bios/dats",
  ];
  for (const route of routes) {
    await page.goto(route);
    await expect(page.locator(".page-header")).toBeVisible();
    await noPageOverflow(page);
    await expect(page.getByRole("main")).toBeVisible();
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
  await expect(page.getByRole("heading", { name: "任务进度", exact: true })).toBeVisible();
  await expect(page.getByText("技术详情", { exact: true })).toHaveCount(0);
  await page.goto("/admin/games");
  await expect(page.getByRole("heading", { name: "游戏管理", exact: true })).toBeVisible();
  await expect(page.getByText("信息版本", { exact: true })).toHaveCount(0);
  const adminGameCard = page.locator(".admin-game-card").first();
  await expect(adminGameCard).toBeVisible();
  expect((await adminGameCard.boundingBox())?.width ?? 0).toBeGreaterThanOrEqual(219);
  await page.goto("/admin/bios/dats");
  await expect(page.getByRole("heading", { name: "街机数据目录", exact: true })).toBeVisible();
  await expect(page.getByText("技术详情", { exact: true })).toHaveCount(0);
  await page.goto("/admin/platform-instances");
  await expect(page.getByRole("columnheader", { name: "启用状态" })).toBeVisible();
  const populatedDirectory = page.locator(".platform-table tbody tr").filter({ hasText: /[1-9]\d* 个游戏/ }).first();
  if (await populatedDirectory.count()) await expect(populatedDirectory.getByRole("checkbox", { name: /启用状态/ })).toBeEnabled();
  const descriptionRow = page.locator(".platform-table tbody tr").first();
  const descriptionEdit = descriptionRow.getByRole("button", { name: /给用户看的说明/ });
  if (await descriptionEdit.count()) {
    const before = await descriptionRow.evaluate((element) => element.getBoundingClientRect().height);
    await descriptionEdit.click();
    await expect(descriptionRow.getByRole("textbox", { name: "给用户看的说明" })).toHaveAttribute("rows", "1");
    const after = await descriptionRow.evaluate((element) => element.getBoundingClientRect().height);
    expect(Math.abs(after - before)).toBeLessThanOrEqual(4);
    await descriptionRow.getByRole("button", { name: "取消修改说明" }).click();
  }
  const games = await page.request.get("/api/v1/admin/games?limit=100");
  const payload = await games.json() as { items: Array<{ gameId: string }> };
  if (payload.items[0]) {
    await page.goto(`/admin/games/${payload.items[0].gameId}`);
    for (const heading of ["发布信息", "媒体", "游戏内容与运行环境", "管理操作"]) {
      await expect(page.getByRole("heading", { name: heading })).toBeVisible();
    }
    await noPageOverflow(page);
  }
  await page.screenshot({ path: evidencePath(testInfo, "admin-layout.png"), fullPage: true });
});

test("ACC-UI-007 keyboard focus and reduced motion remain explicit", async ({ page }, testInfo) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/library");
  await page.keyboard.press("Tab");
  expect(await page.evaluate(() => document.activeElement?.tagName)).not.toBe("BODY");
  const reducedDuration = await page.locator(".game-card").first().evaluate((element) => getComputedStyle(element).transitionDuration);
  expect(Number.parseFloat(reducedDuration)).toBeLessThanOrEqual(0.01);
  const focusable = page.locator("a,button,input,select");
  expect(await focusable.count()).toBeGreaterThan(5);
  for (const control of await page.locator("button").all()) await expect(control).toHaveAccessibleName(/.+/);
  await page.screenshot({ path: evidencePath(testInfo, "keyboard-reduced-motion.png"), fullPage: true });
});

test("ACC-UI-008 large review queue preserves filters, pagination, draft safety, and decisions", async ({ page }, testInfo) => {
  const primaryJob = "20000000-0000-7000-8000-000000000001";
  const itemId = (number: number) => `30000000-0000-7000-8001-${String(number).padStart(12, "0")}`;
  await page.goto("/admin/imports/tasks");
  const primaryRow = page.getByRole("row").filter({ hasText: "60 / 0" });
  await expect(primaryRow).toBeVisible();
  await primaryRow.getByRole("link", { name: "查看待审核" }).click();
  await expect(page).toHaveURL(new RegExp(`importJobId=${primaryJob}`));
  await expect(page.getByRole("textbox", { name: "导入批次" })).toHaveValue(primaryJob);
  const rows = page.locator("[data-review-item]");
  await expect(rows).toHaveCount(50);
  await page.getByRole("button", { name: "加载更多待审条目" }).click();
  await expect(rows).toHaveCount(60);
  expect(new Set(await rows.evaluateAll((elements) => elements.map((element) => element.getAttribute("data-review-item")))).size).toBe(60);
  await expect(rows.first()).toContainText("可以发布");
  await expect(page.getByRole("button", { name: /批量/ })).toHaveCount(0);

  await page.getByRole("link", { name: "清除" }).click();
  await expect(page).toHaveURL(/\/admin\/reviews$/);
  await expect(rows).toHaveCount(50);
  await page.getByRole("button", { name: "加载更多待审条目" }).click();
  await expect(rows).toHaveCount(63);
  await page.goBack();
  await expect(page).toHaveURL(new RegExp(`importJobId=${primaryJob}`));
  await expect(rows).toHaveCount(60);

  const item57 = page.locator(`[data-review-item="${itemId(57)}"]`).getByRole("link", { name: "审核条目" });
  await item57.focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(new RegExp(`/admin/reviews/${itemId(57)}`));
  await page.setViewportSize({ width: 3840, height: 2160 });
  await expect(page.getByRole("navigation", { name: "当前待审队列" })).toBeVisible();
  expect(await page.locator(".review-detail-workbench").evaluate((element) => getComputedStyle(element).gridTemplateColumns.split(" ").length)).toBe(2);
  await page.screenshot({ path: evidencePath(testInfo, "review-workbench-4k.png"), fullPage: true });

  await page.getByRole("textbox", { name: "标题" }).fill("尚未保存的标题");
  await page.getByRole("navigation", { name: "当前待审队列" }).getByRole("link", { name: /Batch 1 Game 03/ }).click();
  await expect(page.getByRole("alertdialog", { name: "草稿还没有保存" })).toBeVisible();
  await page.getByRole("button", { name: "留在页面" }).click();
  await expect(page).toHaveURL(new RegExp(`/admin/reviews/${itemId(57)}`));
  await page.getByRole("button", { name: "保存草稿" }).click();
  await expect(page.getByRole("status")).toContainText("草稿及来源选择已保存");
  await page.getByRole("navigation", { name: "当前待审队列" }).getByRole("link", { name: /Batch 1 Game 03/ }).click();
  await expect(page).toHaveURL(new RegExp(`/admin/reviews/${itemId(3)}`));
  await page.getByRole("textbox", { name: "标题" }).fill("Batch 1 Game 03 Saved");
  await page.getByRole("button", { name: "保存草稿" }).click();
  await expect(page.getByRole("status")).toContainText("草稿及来源选择已保存");

  await page.setViewportSize({ width: 1280, height: 800 });
  await expect(page.getByRole("navigation", { name: "当前待审队列" })).toBeHidden();
  await page.screenshot({ path: evidencePath(testInfo, "review-detail-1280.png"), fullPage: true });
  await page.getByRole("button", { name: "通过并发布" }).click();
  await expect(page).not.toHaveURL(new RegExp(`/admin/reviews/${itemId(3)}(?:\\?|$)`));
  await expect(page.locator(".app-toast")).toContainText("游戏已成功发布");

  await page.getByRole("link", { name: "返回待审核列表" }).click();
  await expect(page).toHaveURL(new RegExp(`importJobId=${primaryJob}`));
  await expect(rows).toHaveCount(50);
  await page.getByRole("button", { name: "加载更多待审条目" }).click();
  await expect(rows).toHaveCount(59);
  await page.locator(`[data-review-item="${itemId(58)}"]`).getByRole("link", { name: "审核条目" }).click();
  await page.getByRole("textbox", { name: "丢弃原因" }).fill("验收：明确丢弃单个条目");
  await page.getByRole("button", { name: "丢弃条目" }).click();
  await expect(page).not.toHaveURL(new RegExp(`/admin/reviews/${itemId(58)}(?:\\?|$)`));
  await expect(page.locator(".app-toast")).toContainText("条目已丢弃");

  const response = await page.request.get(`/api/v1/admin/reviews?importJobId=${primaryJob}&limit=100`);
  expect(response.ok()).toBe(true);
  const remaining = await response.json() as { items: Array<{ itemId: string }> };
  expect(remaining.items).toHaveLength(58);
  expect(remaining.items.some((item) => item.itemId === itemId(3) || item.itemId === itemId(58))).toBe(false);
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
  await page.locator(".game-card").filter({ hasText: "Sudoku" }).click();
  const configResponse = page.waitForResponse((response) => /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  const configuration = await (await configResponse).json() as { emulatorGameId: number; gameName: string; gameUrl: string; loaderUrl: string; runtimePathOverrides: Record<string, string>; playerAdapterId: string; emulatorjsVersion: string };
  expect(Number.isSafeInteger(configuration.emulatorGameId) && configuration.emulatorGameId > 0).toBe(true);
  expect(configuration.gameName).toBe(`retrom-${configuration.emulatorGameId}`);
  expect(configuration.playerAdapterId).toBe("ejs-4.2.3-v1");
  expect(configuration.emulatorjsVersion).toBe("4.2.3");
  expect(configuration.gameUrl).not.toMatch(/(?:blob:|file:|\/home\/)/);
  await expect(page.locator(".player-shell")).toBeVisible();
  await expect(page.getByRole("button", { name: "开始游戏" })).toHaveCount(0);
  await expect(page.locator(".player-loading")).toBeHidden({ timeout: 30_000 });
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
  await page.locator(".game-card").filter({ hasText: "Sudoku" }).click();
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
  await page.locator(".game-card").filter({ hasText: "Sudoku" }).click();
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  await expect(page.getByText("BIOS Hash 与目录期望不一致，已按 Warning 继续运行。", { exact: true })).toBeVisible();
  await expect(page.locator(".player-loading")).toBeHidden({ timeout: 30_000 });
  await expect(page.getByRole("button", { name: "开始游戏" })).toHaveCount(0);
  await page.screenshot({ path: evidencePath(testInfo, "bios-hash-warning-autostart.png"), fullPage: true });
  await page.getByRole("button", { name: "退出游戏" }).click();
  await expect(page).toHaveURL(/\/games\/[0-9a-f-]+$/);

  await page.goto("/library");
  await page.locator(".game-card").filter({ hasText: "Acceptance Missing FDS BIOS" }).click();
  await expect(page).toHaveURL(/\/games\/60000000-0000-7000-8000-000000000001$/);
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page.locator(".launch-panel [role=alert]")).toContainText("LAUNCH_BIOS_MISSING", { timeout: 30_000 });
  await expect(page.getByRole("link", { name: "前往 BIOS 管理" })).toBeVisible();
  await expect(page).toHaveURL(/\/games\/60000000-0000-7000-8000-000000000001$/);
  expect(await page.evaluate(() => sessionStorage.getItem("retrom:fullscreen-requested"))).toBe("true");
  expect(await page.evaluate(() => sessionStorage.getItem("retrom:fullscreen-exited"))).toBe("true");
  await page.screenshot({ path: evidencePath(testInfo, "required-bios-blocker-repair.png"), fullPage: true });
});

test("ACC-SAVE-002 detail, saves, and home resume the locked save in one click", async ({ page }, testInfo) => {
  await page.addInitScript(() => {
    Object.defineProperty(Element.prototype, "requestFullscreen", { configurable: true, value: () => Promise.resolve() });
  });
  await page.goto("/library");
  await page.locator(".game-card").filter({ hasText: "Sudoku" }).click();
  await expect(page).toHaveURL(/\/games\/[0-9a-f-]+$/);
  const detailURL = page.url();
  const gameId = detailURL.split("/").at(-1)!;
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  const launchId = page.url().split("/").at(-1)!;
  const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64");
  const saveResponse = await page.request.post(`/runtime/launches/${launchId}/save-states`, {
    headers: { "Idempotency-Key": crypto.randomUUID() },
    multipart: {
      metadata: { name: "metadata.json", mimeType: "application/json", buffer: Buffer.from('{"name":"Acceptance Save"}') },
      state: { name: "state.bin", mimeType: "application/octet-stream", buffer: Buffer.from([1, 2, 3, 4]) },
      screenshot: { name: "screenshot.png", mimeType: "image/png", buffer: png },
    },
  });
  expect(saveResponse.status()).toBe(201);
  const saved = await saveResponse.json() as { saveStateId: string };

  await page.goto(detailURL);
  await page.getByRole("button", { name: "从此存档继续" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  await page.goto("/saves");
  await page.getByRole("button", { name: "从这里继续" }).first().click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  await page.goto("/");
  await page.getByRole("button", { name: "继续此存档" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);

  const mismatch = await page.request.post("/api/v1/launches", {
    headers: { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() },
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
  await expect(page.getByRole("heading", { name: "已选择 1 个文件" })).toBeVisible();
  await page.locator("#provider").selectOption("NONE");
  await page.getByRole("button", { name: "上传、验证并创建导入任务" }).click();
  await expect(page).toHaveURL(/\/admin\/reviews\?importJobId=[0-9a-f-]+$/, { timeout: 30_000 });
});
