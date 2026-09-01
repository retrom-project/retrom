import { mkdirSync } from "node:fs";
import path from "node:path";
import axe from "axe-core";
import { expect, test, type Page, type TestInfo } from "@playwright/test";

test.beforeEach(async ({ page }, testInfo) => {
  if (testInfo.title.startsWith("ACC-BIOS-007") && testInfo.project.name !== "chrome-1280") {
    test.skip(true, "FULL_CATALOG 行为只消费一次共享 catalog；多尺寸由 ACC-BIOS-006 覆盖");
  }
  if (testInfo.title.startsWith("ACC-PEG-006") && testInfo.project.name !== "chrome-1280") {
    test.skip(true, "真实 Pegasus 核心链路只执行一次；多尺寸布局由 ACC-PEG-005 覆盖");
  }
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
  return path.join(screenshots, `${testInfo.project.name}-${name}`);
}

async function expectNoPageOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
}

async function expectNoSeriousAxeViolations(page: Page) {
  await page.evaluate(axe.source);
  const violations = await page.evaluate(async () => {
    const axeAPI = (window as typeof window & { axe: { run: (root: Document, options: object) => Promise<{ violations: Array<{ id: string; impact: string | null; nodes: unknown[] }> }> } }).axe;
    const result = await axeAPI.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    return result.violations.filter((violation) => violation.impact === "serious" || violation.impact === "critical");
  });
  expect(violations, `axe serious/critical violations: ${JSON.stringify(violations)}`).toEqual([]);
}

test("ACC-BIOS-006 server import drawer, recovery detail, keyboard and desktop layouts", async ({ page }, testInfo) => {
  await page.goto("/admin/imports/server");
  await expect(page.getByRole("heading", { name: "从服务器目录导入" })).toBeVisible();
  await expect(page.getByText("Pegasus BIOS", { exact: true }).first()).toBeVisible();
  await expectNoPageOverflow(page);

  const trigger = page.getByRole("button", { name: "选择目录并开始" });
  await trigger.focus();
  await trigger.click();
  const drawer = page.getByRole("dialog", { name: "选择 BIOS 所在目录" });
  await expect(drawer).toBeVisible();
  await expect(page.getByRole("checkbox", { name: /允许使用更优候选替换已有 BIOS/ })).not.toBeChecked();
  await expect(drawer).not.toContainText("/tmp/");
  await page.keyboard.press("Shift+Tab");
  expect(await drawer.evaluate((element) => element.contains(document.activeElement))).toBe(true);
  await page.keyboard.press("Escape");
  await expect(drawer).toHaveCount(0);
  await expect(trigger).toBeFocused();

  await trigger.click();
  await page.getByRole("button", { name: /^BIOS/ }).click();
  await expect(drawer.getByText("Pegasus BIOS / BIOS", { exact: true })).toBeVisible();
  await drawer.getByRole("button", { name: "开始异步导入" }).click();
  await expect(page).toHaveURL(/\/admin\/imports\/server\/[0-9a-f-]+$/);
  await expect(page.getByText("已完成", { exact: true }).first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("region", { name: "服务器导入摘要" })).toBeVisible();
  await expect(page.getByRole("table", { name: "BIOS 导入结果" })).toBeVisible();
  await expectNoPageOverflow(page);
  await expectNoSeriousAxeViolations(page);
  await page.screenshot({ path: evidencePath(testInfo, "server-bios-import-detail.png"), fullPage: true });
});

test("ACC-BIOS-007 FULL_CATALOG traverses 100/100/86 and retries the same cursor", async ({ page }, testInfo) => {
  const sizes: number[] = [];
  const ids = new Set<string>();
  let cursor: string | null = null;
  do {
    const query = new URLSearchParams({ scope: "FULL_CATALOG", limit: "100" });
    if (cursor) {query.set("cursor", cursor);}
    const response = await page.request.get(`/api/v1/admin/bios?${query}`);
    expect(response.ok()).toBe(true);
    const payload = await response.json() as { items: Array<{ id: string }>; nextCursor: string | null; filteredCount: number; summary: { totalCount: number } };
    sizes.push(payload.items.length);
    payload.items.forEach((item) => ids.add(item.id));
    expect(payload.filteredCount).toBe(286);
    expect(payload.summary.totalCount).toBe(286);
    cursor = payload.nextCursor;
  } while (cursor);
  expect(sizes).toEqual([100, 100, 86]);
  expect(ids.size).toBe(286);

  let failedOnce = false;
  const cursorRequests: string[] = [];
  await page.route((url) => url.pathname === "/api/v1/admin/bios" && url.searchParams.has("cursor"), async (route) => {
    cursorRequests.push(route.request().url());
    if (!failedOnce) {
      failedOnce = true;
      await route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ error: { code: "INTERNAL_ERROR", message: "injected next-page failure" } }) });
      return;
    }
    await route.continue();
  });
  await page.goto("/admin/bios?scope=FULL_CATALOG");
  await expect(page.getByText("已加载 100 / 286 项", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "加载更多" }).click();
  await expect(page.getByRole("button", { name: "重试加载下一页" })).toBeVisible();
  await expect(page.getByText("已加载 100 / 286 项", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "重试加载下一页" }).click();
  await expect(page.getByText("已加载 200 / 286 项", { exact: true })).toBeVisible();
  expect(new URL(cursorRequests[0]).searchParams.get("cursor")).toBe(new URL(cursorRequests[1]).searchParams.get("cursor"));
  await page.getByRole("button", { name: "加载更多" }).click();
  await expect(page.getByText("已加载全部 286 项", { exact: true })).toBeVisible();
  await expect(page.getByRole("row")).toHaveCount(286);
  await expectNoPageOverflow(page);
  await page.screenshot({ path: evidencePath(testInfo, "bios-full-catalog-286.png"), fullPage: true });
});

test("ACC-PEG-005 three-step Pegasus import recovers and remains bounded at desktop viewports", async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  const batchTagName = `飞行游戏-${testInfo.project.name}`;
  await page.goto("/admin/tags");
  await page.getByRole("button", { name: "新建标签" }).click();
  const createTagDrawer = page.getByRole("dialog", { name: "新建标签" });
  await createTagDrawer.getByRole("textbox", { name: "标签名称" }).fill(batchTagName);
  await createTagDrawer.getByRole("button", { name: "保存标签" }).click();
  await expect(page.getByRole("rowheader", { name: batchTagName })).toBeVisible();
  await page.goto("/admin/imports/server");
  await expect(page.getByRole("heading", { name: "扫描并导入 BIOS" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "扫描并准备审核事项" })).toBeVisible();
  await expect(page.getByText("Pegasus BIOS", { exact: true }).first()).toBeVisible();
  await expectNoPageOverflow(page);

  const trigger = page
    .locator(".pegasus-capability")
    .getByRole("button", { name: /选择目录并扫描|继续扫描或映射/ });
  await trigger.click();
  let drawer = page.getByRole("dialog", { name: "从 Pegasus 目录准备审核事项" });
  await expect(drawer).toBeVisible();
  await expect(drawer.getByRole("list", { name: "导入步骤" })).toContainText("选择目录");
  await drawer.getByRole("button", { name: /^Games/ }).click();
  await expect(drawer).toContainText("Pegasus BIOS / Games");
  const scanResponse = page.waitForResponse((response) => new URL(response.url()).pathname === "/api/v1/admin/pegasus-imports" && response.request().method() === "POST");
  await drawer.getByRole("button", { name: "扫描此目录" }).click();
  const createdPlan = await (await scanResponse).json() as { id: string };
  const footerClose = drawer.locator("footer").getByRole("button", { name: "关闭", exact: true });
  await expect(footerClose).toBeEnabled();
  await footerClose.click();
  await expect(drawer).toHaveCount(0);

  await expect.poll(async () => {
    const response = await page.request.get(`/api/v1/admin/pegasus-imports/${createdPlan.id}`);
    const payload = await response.json() as { state: string };
    return payload.state;
  }, { timeout: 30_000 }).toBe("AWAITING_MAPPING");
  await page.goto(`/admin/imports/server/pegasus/${createdPlan.id}`);
  await expect(page.getByText("等待映射", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "继续映射" }).click();
  drawer = page.getByRole("dialog", { name: "从 Pegasus 目录准备审核事项" });
  const mapping = drawer.getByRole("combobox", { name: "NES 处理方式" });
  await expect(mapping).toBeVisible({ timeout: 30_000 });
  await expect(mapping).toHaveValue("");
  await expect(drawer.getByRole("button", { name: "确认映射" })).toBeDisabled();
  const batchTags = drawer.getByRole("combobox", { name: "批次标签" });
  await batchTags.fill(batchTagName);
  await page.keyboard.press("Enter");
  await drawer.getByRole("button", { name: "应用到所有未跳过 Collection" }).click();
  await expect(drawer.getByRole("status")).toContainText("覆盖 1 个游戏");
  const nesOption = mapping.getByRole("option", { name: /^导入到 NES 游戏/ });
  await mapping.selectOption(await nesOption.getAttribute("value") ?? "");
  await expect(drawer.getByRole("button", { name: `移除标签“${batchTagName}”` }).last()).toBeVisible();
  const collectionTags = drawer.getByRole("combobox", { name: "NES 的默认标签" });
  await collectionTags.focus();
  const floatingTagList = page.getByRole("listbox");
  await expect(floatingTagList).toBeVisible();
  expect(await floatingTagList.evaluate((element) => element.parentElement === document.body)).toBe(true);
  const floatingBounds = await floatingTagList.boundingBox();
  expect(floatingBounds).not.toBeNull();
  expect(floatingBounds!.y).toBeGreaterThanOrEqual(0);
  expect(floatingBounds!.y + floatingBounds!.height).toBeLessThanOrEqual(page.viewportSize()!.height);
  await page.keyboard.press("Escape");
  await expect(floatingTagList).toHaveCount(0);
  await drawer.getByRole("button", { name: "确认映射" }).click();
  await expect(drawer).toContainText("1 个处理 · 0 个跳过");
  await expect(drawer).toContainText("1 个 Collection · 1 个游戏");
  await drawer.getByRole("button", { name: "开始准备审核事项" }).click();
  await expect(page).toHaveURL(/\/admin\/imports\/server\/pegasus\/[0-9a-f-]+$/);
  await expect(page.getByRole("region", { name: "Pegasus 导入摘要" })).toBeVisible();
  await expect(page.getByText(/^(审核事项已生成|部分失败)$/).first()).toBeVisible({ timeout: 60_000 });
  const resultTable = page.getByRole("table", { name: "Pegasus 导入结果" });
  await expect(resultTable).toContainText("Acceptance Game");
  await expect(resultTable).toContainText(batchTagName);
  await expect(resultTable).toContainText("待管理员审核");
  await expect(resultTable).toContainText("视频 READY");
  const adminGamesResponse = await page.request.get("/api/v1/admin/games?q=Acceptance%20Game&limit=100");
  expect(adminGamesResponse.ok()).toBe(true);
  const adminGames = await adminGamesResponse.json() as { items: Array<{ title: string }> };
  expect(adminGames.items.some((item) => item.title === "Acceptance Game")).toBe(false);
  const reviewBatchLink = page.getByRole("link", { name: /逐项审核 \d+ 个游戏/ });
  await expect(reviewBatchLink).toHaveAttribute("href", `/admin/reviews?pegasusImportId=${createdPlan.id}`);
  await reviewBatchLink.click();
  await expect(page).toHaveURL(new RegExp(`/admin/reviews\\?pegasusImportId=${createdPlan.id}$`));
  await expect(page.getByRole("heading", { name: "审核这批 Pegasus 游戏" })).toBeVisible();
  await expect(page.getByRole("heading", { name: /^Acceptance Game/ })).toBeVisible();
  await page.goto(`/admin/imports/server/pegasus/${createdPlan.id}`);
  await page.getByRole("searchbox", { name: "搜索标题" }).fill("Acceptance");
  await page.getByRole("button", { name: "应用筛选" }).click();
  await expect(page).toHaveURL(/q=Acceptance/);
  await page.route(`**/api/v1/admin/pegasus-imports/${createdPlan.id}/items?**`, async (route) => {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({
      items: [{
        id: "77777777-7777-4777-8777-777777777777", title: "1944 循环的征服者",
        collectionId: "66666666-6666-4666-8666-666666666666", collectionName: "飞机街机",
        targetPlatformInstanceId: "02980000-0000-7000-8000-000000000106", targetPlatformInstanceName: "FBNeo 游戏",
        metadataRelativePath: "metadata.pegasus.txt", executionState: "REVIEW_PENDING", contentKind: "SINGLE_FILE",
        tags: [],
        media: { cover: "READY", video: "MISSING" }, warnings: [], discoveryCode: null,
        errorCode: null, retryable: false, publishedGameId: null, existingGameId: null,
        reviewItemId: "99999999-9999-4999-8999-999999999999",
        failureDetails: null, existingMatches: [], updatedAtMs: Date.now(),
        runtimeCheck: {
          status: "BLOCKED", code: "LAUNCH_PARENT_MISSING", coreId: "fbneo", coreName: "FinalBurn Neo",
          machine: "1944j", missingEntries: ["1944.zip"], mismatchedEntries: [], bios: [], missingDiscs: [],
          dependencies: [{ kind: "PARENT", machine: "1944", requiredBy: "1944j", expectedLogicalName: "1944.zip", state: "MISSING", requiredEntries: ["nffe.03"] }],
        },
      }, {
        id: "88888888-8888-4888-8888-888888888888", title: "1944 内部组装失败",
        collectionId: "66666666-6666-4666-8666-666666666666", collectionName: "飞机街机",
        targetPlatformInstanceId: "02980000-0000-7000-8000-000000000106", targetPlatformInstanceName: "FBNeo 游戏",
        metadataRelativePath: "metadata.pegasus.txt", executionState: "COMMIT_FAILED", contentKind: "SINGLE_FILE",
        tags: [],
        media: { cover: "READY", video: "READY" }, warnings: [], discoveryCode: null,
        errorCode: "PEGASUS_LIBRARY_IMPORT_FAILED", retryable: true, publishedGameId: null, existingGameId: null,
        existingMatches: [], updatedAtMs: Date.now(), runtimeCheck: null,
        failureDetails: {
          schemaVersion: 1, stage: "LIBRARY_IMPORT", operation: "CREATE_SERVER_SOURCE",
          causeCode: "SOURCE_FILE_LIMIT_EXCEEDED",
          technicalDetail: "Pegasus assembled 109 source files for one Arcade item; library import accepts at most 64.",
          relativePath: "1944j.zip", observedFileCount: 109, allowedFileCount: 64,
          libraryImportJobId: null, libraryImportItemId: null,
        },
      }], nextCursor: null,
    }) });
  });
  await page.getByRole("searchbox", { name: "搜索标题" }).fill("1944");
  await page.getByRole("button", { name: "应用筛选" }).click();
  await expect(resultTable).toContainText("缺少父 ROM");
  const runtimeRow = resultTable.getByRole("row").filter({ hasText: "1944 循环的征服者" });
  await runtimeRow.getByText("查看具体原因与处理建议").click();
  await expect(runtimeRow).toContainText("LAUNCH_PARENT_MISSING");
  await expect(runtimeRow).toContainText("1944.zip");
  await expect(runtimeRow).toContainText("把缺失的父 ROM ZIP 放入同一 Pegasus 来源");
  const internalFailureRow = resultTable.getByRole("row").filter({ hasText: "1944 内部组装失败" });
  await expect(internalFailureRow).toContainText("Arcade companion 候选数量超过内部上限");
  await internalFailureRow.getByText("查看具体原因与处理建议").click();
  await expect(internalFailureRow).toContainText("SOURCE_FILE_LIMIT_EXCEEDED");
  await expect(internalFailureRow).toContainText("CREATE_SERVER_SOURCE");
  await expect(internalFailureRow).toContainText("109 / 上限 64");
  await expect(internalFailureRow).toContainText("1944j.zip");
  await expectNoPageOverflow(page);
  await expectNoSeriousAxeViolations(page);
  await page.screenshot({ path: evidencePath(testInfo, "pegasus-import-detail.png"), fullPage: true });
});

test("ACC-PEG-006 project-owned Pegasus GBA source publishes and advances real emulator frames", async ({ page }, testInfo) => {
  test.setTimeout(180_000);
  await page.addInitScript(() => {
    Object.defineProperty(Element.prototype, "requestFullscreen", {
      configurable: true,
      value: () => Promise.resolve(),
    });
  });

  const title = "Pegasus GBA Smoke";
  const beforeGamesResponse = await page.request.get(`/api/v1/admin/games?q=${encodeURIComponent(title)}&limit=100`);
  expect(beforeGamesResponse.ok()).toBe(true);
  const beforeGames = await beforeGamesResponse.json() as { items: Array<{ gameId: string; title: string }> };
  expect(beforeGames.items.filter((game) => game.title === title)).toHaveLength(0);

  await page.goto("/admin/imports/server");
  await page
    .locator(".pegasus-capability")
    .getByRole("button", { name: /选择目录并扫描|继续扫描或映射/ })
    .click();
  const drawer = page.getByRole("dialog", { name: "从 Pegasus 目录准备审核事项" });
  await drawer.getByRole("button", { name: /^Playable/ }).click();
  await expect(drawer).toContainText("Pegasus BIOS / Playable");
  const scanResponse = page.waitForResponse((response) => new URL(response.url()).pathname === "/api/v1/admin/pegasus-imports" && response.request().method() === "POST");
  await drawer.getByRole("button", { name: "扫描此目录" }).click();
  const plan = await (await scanResponse).json() as { id: string };

  await expect.poll(async () => {
    const response = await page.request.get(`/api/v1/admin/pegasus-imports/${plan.id}`);
    const payload = await response.json() as { state: string };
    return payload.state;
  }, { timeout: 30_000 }).toBe("AWAITING_MAPPING");
  const mapping = drawer.getByRole("combobox", { name: "GBA Smoke 处理方式" });
  await expect(mapping).toBeVisible({ timeout: 30_000 });
  const gbaOption = mapping.getByRole("option", { name: /^导入到 GBA 游戏/ });
  await mapping.selectOption(await gbaOption.getAttribute("value") ?? "");
  await drawer.getByRole("button", { name: "确认映射" }).click();
  await expect(drawer.getByText("可处理 / 源内容阻断").locator("..")).toContainText("1 / 0 个游戏");
  await drawer.getByRole("button", { name: "开始准备审核事项" }).click();

  await expect(page).toHaveURL(new RegExp(`/admin/imports/server/pegasus/${plan.id}$`));
  await expect(page.getByText("审核事项已生成", { exact: true })).toBeVisible({ timeout: 60_000 });
  const resultTable = page.getByRole("table", { name: "Pegasus 导入结果" });
  const resultRow = resultTable.getByRole("row").filter({ hasText: title });
  await expect(resultRow).toContainText("待管理员审核");
  const reviewLink = page.getByRole("link", { name: /逐项审核 1 个游戏/ });
  await expect(reviewLink).toHaveAttribute("href", `/admin/reviews?pegasusImportId=${plan.id}`);
  await reviewLink.click();

  const reviewRow = page.locator(".review-workflow-row").filter({ hasText: title });
  await expect(reviewRow).toContainText("可以发布", { timeout: 30_000 });
  await expect(reviewRow).toContainText("Pegasus · GBA Smoke");
  await reviewRow.getByRole("link", { name: "审核条目" }).click();
  await expect(page.getByRole("heading", { name: "审核条目" })).toBeVisible();
  await expect(page.getByText("来源：Pegasus · GBA Smoke", { exact: true })).toBeVisible();
  const approve = page.getByRole("button", { name: "通过并发布" });
  await expect(approve).toBeEnabled();
  await approve.click();
  await expect(page.locator(".app-toast")).toContainText("游戏已成功发布", { timeout: 20_000 });

  let gameId = "";
  await expect.poll(async () => {
    const response = await page.request.get(`/api/v1/admin/games?q=${encodeURIComponent(title)}&limit=100`);
    const payload = await response.json() as { items: Array<{ gameId: string; title: string }> };
    gameId = payload.items.find((game) => game.title === title)?.gameId ?? "";
    return gameId;
  }, { timeout: 20_000 }).not.toBe("");

  await page.goto(`/games/${gameId}`);
  const configResponse = page.waitForResponse((response) => /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/, { timeout: 10_000 });
  const configuration = await (await configResponse).json() as { gameTitle: string; core: string; coreName: string; playerAdapterId: string; emulatorjsVersion: string; gameUrl: string };
  expect(configuration.gameTitle).toBe(title);
  expect(configuration.core).toBe("mgba");
  expect(configuration.coreName).toBe("mGBA");
  expect(configuration.playerAdapterId).toBe("ejs-4.2.3-v3");
  expect(configuration.emulatorjsVersion).toBe("4.2.3");
  expect(configuration.gameUrl).toMatch(
    /\/runtime\/content\/game\/[0-9a-f]{64}\/pegasus-smoke\.gba$/,
  );

  const player = page.frameLocator("iframe.player-frame");
  await expect(player.locator("canvas.ejs_canvas")).toBeVisible({ timeout: 60_000 });
  const playerFrame = page.frames().find((frame) => frame !== page.mainFrame());
  expect(playerFrame).toBeTruthy();
  const initialFrame = await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0);
  await expect.poll(
    async () => playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0),
    { timeout: 30_000 },
  ).toBeGreaterThan(initialFrame + 30);
  await page.mouse.move(20, 20);
  await expect(page.locator(".player-game-meta")).toContainText(title);
  await page.screenshot({ path: evidencePath(testInfo, "pegasus-gba-player-running.png"), fullPage: true });
});

test("ACC-MEDIA-001 video upload is explicit in admin and absent from library requests", async ({ page }, testInfo) => {
  test.setTimeout(90_000);
  const gamesResponse = await page.request.get("/api/v1/admin/games?limit=100");
  expect(gamesResponse.ok()).toBe(true);
  const games = await gamesResponse.json() as { items: Array<{ gameId: string; title: string }> };
  const game = games.items.find((item) => item.title === "Sudoku");
  if (!game) {throw new Error("acceptance fixture game Sudoku was not found");}
  const gameId = game.gameId;

  await page.goto(`/admin/games/${gameId}`);
  await expect(page.getByRole("heading", { name: "背景图" })).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "游戏截图" })).toHaveCount(0);
  const upload = page.locator("#admin-video-upload");
  await expect(upload).toBeEnabled();
  await upload.setInputFiles({
    name: "acceptance.mp4",
    mimeType: "video/mp4",
    buffer: Buffer.from([0, 0, 0, 24, 102, 116, 121, 112, 105, 115, 111, 109, 0, 0, 0, 0, 105, 115, 111, 109, 109, 112, 52, 50]),
  });
  const adminVideo = page.getByLabel(/管理视频预览/);
  await expect(adminVideo).toBeVisible();
  await expect(adminVideo).toHaveAttribute("controls", "");
  await expect(adminVideo).not.toHaveAttribute("autoplay", "");
  const mediaLayout = await page.locator(".admin-game-media-grid").evaluate((element) => {
    const body = element.getBoundingClientRect();
    const style = getComputedStyle(element);
    const cover = element.querySelector<HTMLElement>(".admin-game-cover-slot")!.getBoundingClientRect();
    const video = element.querySelector<HTMLElement>(".admin-game-video-slot")!.getBoundingClientRect();
    const preview = element.querySelector<HTMLVideoElement>("video")!;
    const previewFrame = preview.parentElement!.getBoundingClientRect();
    const previewRect = preview.getBoundingClientRect();
    const previewStyle = getComputedStyle(preview);
    return {
      rightGap: body.right - Number.parseFloat(style.paddingRight) - video.right,
      topGap: video.top - body.top - Number.parseFloat(style.paddingTop),
      bottomGap: body.bottom - Number.parseFloat(style.paddingBottom) - video.bottom,
      coverWidth: cover.width,
      videoWidth: video.width,
      previewHorizontalOffset: (previewRect.left + previewRect.right - previewFrame.left - previewFrame.right) / 2,
      previewVerticalOffset: (previewRect.top + previewRect.bottom - previewFrame.top - previewFrame.bottom) / 2,
      display: previewStyle.display,
      placeSelf: previewStyle.placeSelf,
      objectFit: previewStyle.objectFit,
      objectPosition: previewStyle.objectPosition,
    };
  });
  expect(Math.abs(mediaLayout.rightGap)).toBeLessThanOrEqual(1);
  expect(Math.abs(mediaLayout.topGap)).toBeLessThanOrEqual(1);
  expect(Math.abs(mediaLayout.bottomGap)).toBeLessThanOrEqual(1);
  expect(mediaLayout.videoWidth).toBeGreaterThan(mediaLayout.coverWidth);
  expect(Math.abs(mediaLayout.previewHorizontalOffset)).toBeLessThanOrEqual(1);
  expect(Math.abs(mediaLayout.previewVerticalOffset)).toBeLessThanOrEqual(1);
  expect(mediaLayout.display).toBe("block");
  expect(mediaLayout.placeSelf).toBe("center");
  expect(mediaLayout.objectFit).toBe("contain");
  expect(mediaLayout.objectPosition).toBe("50% 50%");
  if ((page.viewportSize()?.width ?? 0) >= 1400) {
    const panelHeights = await page.locator(".admin-game-primary-grid").evaluate((element) => ({
      publish: element.querySelector<HTMLElement>(".admin-game-publish")!.getBoundingClientRect().height,
      media: element.querySelector<HTMLElement>(".admin-game-media")!.getBoundingClientRect().height,
    }));
    expect(Math.abs(panelHeights.publish - panelHeights.media)).toBeLessThanOrEqual(1);
  }

  const detailResponse = await page.request.get(`/api/v1/games/${gameId}`);
  expect(detailResponse.ok()).toBe(true);
  const detail = await detailResponse.json() as { videoUrl: string | null };
  expect(detail.videoUrl).toBeTruthy();
  await page.goto("/library");
  await expect(page.getByRole("heading", { name: "游戏库" })).toBeVisible();
  let videoRequests = 0;
  page.on("request", (request) => {
    if (new URL(request.url()).pathname === detail.videoUrl) {videoRequests += 1;}
  });
  await page.reload();
  await expect(page.getByRole("heading", { name: "游戏库" })).toBeVisible();
  expect(videoRequests).toBe(0);

  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto(`/games/${gameId}`);
  const detailVideo = page.getByLabel(/视频预览/);
  await expect(detailVideo).toHaveJSProperty("muted", true);
  await expect(detailVideo).toHaveAttribute("playsinline", "");
  await expect(detailVideo).toHaveAttribute("loop", "");
  await expect(detailVideo).not.toHaveAttribute("autoplay", "");
  await expect(page.getByRole("button", { name: /播放视频预览/ })).toBeVisible();
  await expectNoPageOverflow(page);
  await expectNoSeriousAxeViolations(page);
  await page.screenshot({ path: evidencePath(testInfo, "game-video-reduced-motion.png"), fullPage: true });

  await page.goto(`/admin/games/${gameId}`);
  await page.getByRole("button", { name: "移除" }).click();
  await expect(page.getByText("暂无视频", { exact: true })).toBeVisible();
});

const realBIOSRelativePath = process.env.RETROM_REAL_BIOS_RELATIVE_PATH;

test.describe("authorized real server BIOS source", () => {
  test.skip(!realBIOSRelativePath, "requires an explicitly authorized local BIOS directory");

  test("REAL-BIOS imports authorized files and preserves browser evidence", async ({ page }, testInfo) => {
    test.setTimeout(180_000);
    await page.goto("/admin/imports/server");
    await page.getByRole("button", { name: "选择目录并开始" }).click();
    const drawer = page.getByRole("dialog", { name: "选择 BIOS 所在目录" });
    for (const segment of realBIOSRelativePath!.split("/")) {
      await drawer.getByRole("button", { name: segment, exact: true }).click();
    }
    const escapedRelativePath = realBIOSRelativePath!.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    await expect(drawer.locator("strong").filter({ hasText: new RegExp(`${escapedRelativePath}$`) })).toBeVisible();
    await expect(page.getByRole("checkbox", { name: /允许使用更优候选替换已有 BIOS/ })).not.toBeChecked();
    await drawer.getByRole("button", { name: "开始异步导入" }).click();
    await expect(page).toHaveURL(/\/admin\/imports\/server\/[0-9a-f-]+$/);
    await expect(page.getByText(/^(已完成|部分失败|任务失败)$/).first()).toBeVisible({ timeout: 150_000 });

    const importID = new URL(page.url()).pathname.split("/").at(-1)!;
    const items: Array<{ requirementId: string; logicalName: string; candidateCount: number; selectedRelativePath?: string | null; state: string }> = [];
    let cursor: string | null = null;
    let summary: { state: string; counts: { candidates: number; imported: number; matched: number; warnings: number; failed: number } } | null = null;
    do {
      const query = new URLSearchParams({ limit: "50" });
      if (cursor) {query.set("cursor", cursor);}
      const response = await page.request.get(`/api/v1/admin/server-imports/${importID}?${query}`);
      expect(response.ok()).toBe(true);
      const payload = await response.json() as { summary: typeof summary; items: typeof items; nextCursor: string | null };
      summary = payload.summary;
      items.push(...payload.items);
      cursor = payload.nextCursor;
    } while (cursor);
    expect(summary).not.toBeNull();
    expect(summary!.counts.candidates).toBeGreaterThan(0);
    expect(items.some((item) => item.candidateCount > 0 && item.selectedRelativePath)).toBe(true);

    await expectNoSeriousAxeViolations(page);
    for (const viewport of [
      { width: 1280, height: 800, name: "1280x800" },
      { width: 2560, height: 1440, name: "2560x1440" },
      { width: 3840, height: 2160, name: "3840-css-ultrawide" },
    ]) {
      await page.setViewportSize(viewport);
      await expectNoPageOverflow(page);
      await page.screenshot({ path: evidencePath(testInfo, `real-server-bios-${viewport.name}.png`), fullPage: true });
    }
    const candidateButton = page.getByRole("button", { name: /^查看候选（[1-9]/ }).first();
    await candidateButton.click();
    await expect(page.getByRole("alertdialog", { name: /候选排序/ })).toBeVisible();
    await page.screenshot({ path: evidencePath(testInfo, "real-server-bios-candidates.png"), fullPage: true });
    await testInfo.attach("real-server-summary.json", {
      body: Buffer.from(JSON.stringify({ importID, sourceRelativePath: realBIOSRelativePath, summary, candidateItems: items.filter((item) => item.candidateCount > 0) }, null, 2)),
      contentType: "application/json",
    });
  });
});
