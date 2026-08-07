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
  await expect(page.getByRole("textbox", { name: "搜索" })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "游戏平台" })).toBeVisible();
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
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/, { timeout: 10_000 });
  const player = page.frameLocator('iframe[title="Retrom EmulatorJS Player"]');
  const canvas = player.locator("canvas.ejs_canvas");
  await expect(canvas).toBeVisible({ timeout: 60_000 });
  const playerFrame = page.frames().find((frame) => frame !== page.mainFrame());
  expect(playerFrame).toBeTruthy();
  const initial = await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0);
  await expect.poll(async () => playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0), { timeout: 30_000 }).toBeGreaterThan(initial + 30);
  await page.mouse.move(20, 20);
  await page.getByRole("button", { name: "暂停" }).click();
  const pausedAt = await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0);
  await page.waitForTimeout(350);
  expect(await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0)).toBeLessThanOrEqual(pausedAt + 1);
  await page.getByRole("button", { name: "继续" }).click();
  await expect.poll(async () => playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0), { timeout: 10_000 }).toBeGreaterThan(pausedAt + 5);
  await page.getByRole("button", { name: "保存进度" }).click();
  await expect(page.getByText("手动存档和截图已保存")).toBeVisible({ timeout: 20_000 });
  const saves = await page.request.get("/api/v1/saves?limit=100");
  expect(saves.ok()).toBe(true);
  const savePayload = await saves.json() as { items: Array<{ gameId: string; name: string }> };
  expect(savePayload.items.some((save) => save.gameId === game!.gameId && save.name.startsWith("手动存档"))).toBe(true);
});

test("library grid and management workbench match desktop breakpoints", async ({ page }, testInfo) => {
  const games = await page.request.get("/api/v1/games?limit=100");
  const payload = await games.json() as { items: Array<{ gameId: string }> };
  await page.goto("/library");
  await expect(page.getByRole("heading", { name: "游戏库", exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  if (payload.items.length > 0) {
    const libraryCard = await page.locator(".admin-game-card").first().evaluate((card) => {
      const cardBox = card.getBoundingClientRect();
      const coverBox = card.querySelector(".admin-game-cover")?.getBoundingClientRect();
      return { cardWidth: cardBox.width, coverRatio: coverBox ? coverBox.width / coverBox.height : 0 };
    });
    expect(libraryCard.cardWidth).toBeGreaterThanOrEqual(269);
    expect(libraryCard.cardWidth).toBeLessThanOrEqual(321);
    expect(Math.abs(libraryCard.coverRatio - 0.75)).toBeLessThanOrEqual(0.01);
    await page.goto(`/admin/games/${payload.items[0].gameId}`);
    for (const heading of ["发布信息", "媒体", "游戏内容与运行环境", "管理操作"]) await expect(page.getByRole("heading", { name: heading })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  }
  await page.screenshot({ path: testInfo.outputPath("library-and-game-admin.png"), fullPage: true });
});

test("BIOS, DAT and save controls are labeled and keyboard reachable", async ({ page }, testInfo) => {
  await page.goto("/admin/bios?scope=FULL_CATALOG");
  await expect(page.getByRole("heading", { name: "BIOS 管理" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "搜索" })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  await page.goto("/admin/bios/dats");
  await expect(page.getByRole("heading", { name: "街机数据目录" })).toBeVisible();
  await expect(page.getByText("技术详情", { exact: true })).toHaveCount(0);
  await expect(page.getByLabel("目标核心版本")).toBeVisible();
  await expect(page.getByRole("button", { name: "选择 DAT 或 XML 文件" })).toBeVisible();
  await page.goto("/saves?availability=ALL");
  await expect(page.getByRole("heading", { name: "我的存档" })).toBeVisible();
  const rename = page.getByRole("button", { name: /编辑存档.*的名称/ }).first();
  if (await rename.count()) await expect(rename).toBeVisible();
  await page.keyboard.press("Tab");
  expect(await page.evaluate(() => document.activeElement?.tagName !== "BODY")).toBe(true);
  await page.screenshot({ path: testInfo.outputPath("bios-dat-saves.png"), fullPage: true });
});
