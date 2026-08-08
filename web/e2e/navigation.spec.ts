import { expect, test } from "@playwright/test";

test("user and admin navigation remain usable without page overflow", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("link", { name: "Retrom 首页" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "今天想玩什么？" })).toBeVisible();
  await page.getByRole("link", { name: "管理后台" }).click();
  await expect(page).toHaveURL(/\/admin\/imports$/);
  await expect(page.getByRole("heading", { name: "游戏入库" })).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
  expect(overflow).toBe(false);
});

test("library exposes its filters and empty state", async ({ page }) => {
  await page.goto("/library");
  await expect(page.getByRole("heading", { name: "游戏库", exact: true })).toBeVisible();
  await expect(page.getByRole("searchbox", { name: "搜索游戏" })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "游戏集合" })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "排列顺序" })).toBeVisible();
});

test("HTML CSP uses a fresh nonce and only development enables unsafe-eval", async ({ page }) => {
  const response = await page.goto("/");
  const csp = response?.headers()["content-security-policy"] ?? "";
  expect(csp).toContain("'nonce-");
  expect(csp).toContain("'wasm-unsafe-eval'");
  if (process.env.RETROM_E2E_PRODUCTION === "1") expect(csp).not.toContain("'unsafe-eval'");
  else expect(csp).toContain("'unsafe-eval'");
});

test("one click creates a capability launch and advances real emulator frames", async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  test.skip(testInfo.project.name !== "chrome-1280", "The real core smoke runs once; layout is covered separately at larger viewports.");
  const games = await page.request.get("/api/v1/games");
  const payload = await games.json() as { items: Array<{ gameId: string; title: string }> };
  const game = payload.items.find((item) => item.title === "Sudoku");
  test.skip(!game, "The launchable acceptance fixture has not been imported.");
  await page.goto(`/games/${game!.gameId}`);
  await page.getByRole("button", { name: /^(开始游戏|重新开始游戏)$/ }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/, { timeout: 10_000 });
  const player = page.frameLocator('iframe[title="Retrom EmulatorJS Player"]');
  const canvas = player.locator("canvas.ejs_canvas");
  await expect(canvas).toBeVisible({ timeout: 60_000 });
  const playerFrame = page.frames().find((frame) => frame !== page.mainFrame());
  expect(playerFrame).toBeTruthy();
  const nativeMenu = player.locator(".ejs_menu_bar");
  await page.mouse.move(640, 790);
  await expect(nativeMenu).toBeHidden();
  await expect(player.getByRole("button", { name: "退出模拟器" })).toHaveCount(0);
  const initial = await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0);
  await expect.poll(async () => playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0), { timeout: 30_000 }).toBeGreaterThan(initial + 30);
  await expect.poll(async () => playerFrame!.evaluate(async () => {
    const emulator = window.EJS_emulator;
    if (!emulator?.takeScreenshot) return 0;
    const result = await Promise.race([
      emulator.takeScreenshot("canvas", "png", 1),
      new Promise<null>((resolve) => setTimeout(() => resolve(null), 1_000)),
    ]);
    if (!result?.blob.size) return 0;
    const bitmap = await createImageBitmap(result.blob);
    const canvas = document.createElement("canvas");
    canvas.width = 64;
    canvas.height = 64;
    const context = canvas.getContext("2d", { alpha: false });
    if (!context) return 0;
    context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    bitmap.close();
    const pixels = context.getImageData(0, 0, canvas.width, canvas.height).data;
    let brightPixels = 0;
    for (let index = 0; index < pixels.length; index += 4) {
      if ((pixels[index] + pixels[index + 1] + pixels[index + 2]) / 3 > 8) brightPixels += 1;
    }
    return brightPixels / (pixels.length / 4);
  }), { timeout: 15_000, intervals: [500] }).toBeGreaterThan(0.02);
  await page.mouse.move(20, 20);
  await expect(page.locator(".player-game-meta")).toContainText("Sudoku");
  const saveResponsePromise = page.waitForResponse((response) => response.request().method() === "POST" && /\/runtime\/launches\/[^/]+\/save-states$/.test(response.url()));
  await page.getByRole("button", { name: "创建存档" }).click();
  const saveResponse = await saveResponsePromise;
  expect(saveResponse.status()).toBe(201);
  const pausedForSave = await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0);
  await page.waitForTimeout(350);
  expect(await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0)).toBeLessThanOrEqual(pausedForSave + 1);
  await expect(page.getByText("手动存档和截图已保存")).toBeVisible({ timeout: 20_000 });
  const saves = await page.request.get("/api/v1/saves?limit=100");
  expect(saves.ok()).toBe(true);
  const savePayload = await saves.json() as { items: Array<{ saveStateId: string; gameId: string; name: string; screenshotUrl: string }> };
  const createdSave = savePayload.items.find((save) => save.gameId === game!.gameId && save.name.startsWith("手动存档"));
  expect(createdSave).toBeTruthy();
  const screenshotStats = await page.evaluate(async (screenshotUrl) => {
    const response = await fetch(screenshotUrl, { credentials: "same-origin", cache: "no-store" });
    if (!response.ok) throw new Error(`save screenshot request failed: ${response.status}`);
    const bitmap = await createImageBitmap(await response.blob());
    const canvas = document.createElement("canvas");
    canvas.width = 64;
    canvas.height = 64;
    const context = canvas.getContext("2d", { alpha: false });
    if (!context) throw new Error("2D canvas unavailable");
    context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    bitmap.close();
    const pixels = context.getImageData(0, 0, canvas.width, canvas.height).data;
    let brightPixels = 0;
    let minimum = 255;
    let maximum = 0;
    for (let index = 0; index < pixels.length; index += 4) {
      const luminance = (pixels[index] + pixels[index + 1] + pixels[index + 2]) / 3;
      if (luminance > 8) brightPixels += 1;
      minimum = Math.min(minimum, luminance);
      maximum = Math.max(maximum, luminance);
    }
    return { brightRatio: brightPixels / (pixels.length / 4), luminanceRange: maximum - minimum };
  }, createdSave!.screenshotUrl);
  expect(screenshotStats.brightRatio).toBeGreaterThan(0.02);
  expect(screenshotStats.luminanceRange).toBeGreaterThan(16);

  await canvas.click({ position: { x: 100, y: 100 } });
  await expect.poll(async () => playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0), { timeout: 10_000 }).toBeGreaterThan(pausedForSave + 5);
  await page.mouse.move(20, 20);
  await page.getByRole("button", { name: "更多操作" }).click();
  await page.getByRole("menuitem", { name: "模拟器设置" }).click();
  await expect(page.getByText("已显示 EmulatorJS 工具栏", { exact: true })).toBeVisible();
  await expect(nativeMenu).toBeVisible();
  await expect(player.getByRole("button", { name: "退出模拟器" })).toHaveCount(0);
  await player.getByRole("button", { name: "设置", exact: true }).click();
  const pausedAt = await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0);
  await page.waitForTimeout(350);
  expect(await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0)).toBeLessThanOrEqual(pausedAt + 1);
  await canvas.click({ position: { x: 100, y: 100 } });
  await expect.poll(async () => playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0), { timeout: 10_000 }).toBeGreaterThan(pausedAt + 5);
});

test("library grid and management workbench match desktop breakpoints", async ({ page }, testInfo) => {
  const games = await page.request.get("/api/v1/games?limit=100");
  const payload = await games.json() as { items: Array<{ gameId: string }> };
  await page.goto("/library");
  await expect(page.getByRole("heading", { name: "游戏库", exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  if (payload.items.length > 0) {
    const libraryCard = await page.locator(".library-game-card").first().evaluate((card) => {
      const cardBox = card.getBoundingClientRect();
      const coverBox = card.querySelector(".library-game-cover")?.getBoundingClientRect();
      return { cardWidth: cardBox.width, coverRatio: coverBox ? coverBox.width / coverBox.height : 0 };
    });
    expect(libraryCard.cardWidth).toBeGreaterThanOrEqual(269);
    expect(libraryCard.cardWidth).toBeLessThanOrEqual(321);
    expect(Math.abs(libraryCard.coverRatio - 0.75)).toBeLessThanOrEqual(0.01);
    await page.goto(`/admin/games/${payload.items[0].gameId}`);
    for (const heading of ["发布信息", "媒体", "游戏文件与运行环境", "管理操作"]) await expect(page.getByRole("heading", { name: heading })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  }
  await page.screenshot({ path: testInfo.outputPath("library-and-game-admin.png"), fullPage: true });
});

test("game detail keeps its one-screen hierarchy and opens saves without navigation", async ({ page }, testInfo) => {
  const [gamesResponse, savesResponse] = await Promise.all([
    page.request.get("/api/v1/games?limit=100"),
    page.request.get("/api/v1/saves?availability=ALL&limit=100"),
  ]);
  const games = await gamesResponse.json() as { items: Array<{ gameId: string }> };
  const saves = await savesResponse.json() as { items: Array<{ gameId: string }> };
  const preferredGameId = saves.items[0]?.gameId ?? games.items[0]?.gameId;
  test.skip(!preferredGameId, "No published game is available for the detail layout check.");

  await page.goto(`/games/${preferredGameId}`);
  await expect(page.locator(".game-detail-hero")).toBeVisible();
  await expect(page.locator(".game-detail-info-strip")).toBeVisible();
  await expect(page.getByRole("region", { name: "你的存档" })).toBeVisible();
  expect(await page.locator(".game-detail-save-card").count()).toBeLessThanOrEqual(4);
  const layout = await page.evaluate(() => ({
    overflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
    savesTop: document.querySelector(".game-detail-saves")?.getBoundingClientRect().top ?? Number.POSITIVE_INFINITY,
    savesBottom: document.querySelector(".game-detail-saves")?.getBoundingClientRect().bottom ?? Number.POSITIVE_INFINITY,
    viewportHeight: document.documentElement.clientHeight,
  }));
  expect(layout.overflow).toBe(false);
  if (testInfo.project.name === "chrome-1280") expect(layout.savesTop).toBeLessThan(layout.viewportHeight);
  else expect(layout.savesBottom).toBeLessThanOrEqual(layout.viewportHeight);

  const runtimeButton = page.getByRole("button", { name: /更换/ });
  await runtimeButton.click();
  await expect(page.getByRole("alertdialog", { name: "更换运行方式" })).toBeVisible();
  await page.getByRole("button", { name: "取消" }).click();
  await expect(page.getByRole("alertdialog", { name: "更换运行方式" })).toHaveCount(0);

  const expectedSaveCount = saves.items.filter((save) => save.gameId === preferredGameId).length;
  if (expectedSaveCount > 0) {
    await page.getByRole("button", { name: "查看全部 →" }).click();
    const drawer = page.getByRole("dialog", { name: "全部存档" });
    await expect(drawer).toBeVisible();
    await expect(drawer.locator(".game-detail-drawer-row")).toHaveCount(expectedSaveCount);
    await drawer.getByRole("button", { name: /预览.*存档截图/ }).first().click();
    await expect(page.getByRole("dialog", { name: "存档截图预览" })).toBeVisible();
    await page.getByRole("button", { name: "关闭" }).click();
    await page.getByRole("button", { name: "关闭全部存档" }).click();
    await expect(page).toHaveURL(new RegExp(`/games/${preferredGameId}$`));
  }
  await page.screenshot({ path: testInfo.outputPath("game-detail-one-screen.png"), fullPage: true });
});

test("BIOS, DAT and save controls are labeled and keyboard reachable", async ({ page }, testInfo) => {
  await page.goto("/admin/bios?scope=FULL_CATALOG");
  await expect(page.getByRole("heading", { name: "BIOS 文件" })).toBeVisible();
  await expect(page.getByRole("searchbox", { name: "搜索 BIOS 文件" })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  await page.goto("/admin/bios/dats");
  await expect(page.getByRole("heading", { name: "街机数据目录" })).toBeVisible();
  await expect(page.getByText("技术详情", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "上传新目录" }).click();
  await expect(page.getByLabel("目标运行方式")).toBeVisible();
  await expect(page.getByRole("button", { name: "选择 DAT 或 XML 文件" })).toBeVisible();
  await page.goto("/saves?availability=ALL");
  await expect(page.getByRole("heading", { name: "我的存档" })).toBeVisible();
  const rename = page.getByRole("button", { name: /编辑存档.*的名称/ }).first();
  if (await rename.count()) await expect(rename).toBeVisible();
  await page.keyboard.press("Tab");
  expect(await page.evaluate(() => document.activeElement?.tagName !== "BODY")).toBe(true);
  await page.screenshot({ path: testInfo.outputPath("bios-dat-saves.png"), fullPage: true });
});
