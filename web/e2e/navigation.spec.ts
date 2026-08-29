import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

async function screenshotCenterBrightRatio(page: Page, screenshot: Buffer) {
  return page.evaluate(async (source) => {
    const decoded = atob(source);
    const bytes = Uint8Array.from(decoded, (character) => character.charCodeAt(0));
    const bitmap = await createImageBitmap(new Blob([bytes], { type: "image/png" }));
    const sample = document.createElement("canvas");
    sample.width = 64;
    sample.height = 64;
    const context = sample.getContext("2d", { alpha: false });
    if (!context) {return 0;}
    context.drawImage(
      bitmap,
      bitmap.width * 0.25,
      bitmap.height * 0.25,
      bitmap.width * 0.5,
      bitmap.height * 0.5,
      0,
      0,
      sample.width,
      sample.height,
    );
    bitmap.close();
    const pixels = context.getImageData(0, 0, sample.width, sample.height).data;
    let brightPixels = 0;
    for (let index = 0; index < pixels.length; index += 4) {
      if ((pixels[index] + pixels[index + 1] + pixels[index + 2]) / 3 > 8) {brightPixels += 1;}
    }
    return brightPixels / (pixels.length / 4);
  }, screenshot.toString("base64"));
}

test.beforeEach(async ({ page }) => {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000";
  const response = await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" }, headers: { Origin: origin }
  });
  expect(response.ok()).toBe(true);
});

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

test("account menu closes after clicking elsewhere on the page", async ({ page }) => {
  await page.goto("/");
  const accountMenu = page.locator(".account-menu");

  await accountMenu.locator("summary").click();
  await expect(accountMenu).toHaveAttribute("open", "");

  await page.getByRole("heading", { name: "今天想玩什么？" }).click();
  await expect(accountMenu).not.toHaveAttribute("open", "");
});

test("library exposes its filters and empty state", async ({ page }) => {
  await page.goto("/library");
  await expect(page.getByRole("heading", { name: "游戏库", exact: true })).toBeVisible();
  await expect(page.getByRole("searchbox", { name: "搜索游戏" })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "游戏集合" })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "排列顺序" })).toBeVisible();
});

test("directory import uses an application dialog before requesting local access", async ({ page }) => {
  await page.goto("/admin/imports/new");
  await page.evaluate(() => {
    Object.defineProperty(window, "showDirectoryPicker", {
      configurable: true,
      value: async () => ({
        kind: "directory",
        name: "snes",
        async *entries() {
          yield ["game.sfc", { kind: "file", name: "game.sfc", getFile: async () => new File(["rom"], "game.sfc") }];
        },
      }),
    });
  });
  const trigger = page.getByRole("button", { name: "选择目录" });
  await trigger.click();

  const dialog = page.getByRole("dialog", { name: "选择游戏目录" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "浏览本机目录" }).click();
  await expect(dialog.getByRole("heading", { name: "snes" })).toBeVisible();
  await expect(dialog.getByText("snes/game.sfc", { exact: true })).toBeVisible();
  await expect(page.getByLabel("选择导入目录")).toHaveAttribute("webkitdirectory", "");
  await expect(dialog.getByRole("button", { name: "使用此目录" })).toBeEnabled();
  await dialog.getByRole("button", { name: "使用此目录" }).click();
  await expect(dialog).toBeHidden();

  await trigger.click();
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("button", { name: "使用此目录" })).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(trigger).toBeFocused();
});

test("protected cover images bypass the unauthenticated Next.js optimizer", async ({ page }) => {
  const response = await page.request.get("/api/v1/games?limit=100");
  const payload = await response.json() as { items: Array<{ title: string; coverUrl: string | null }> };
  const coveredGame = payload.items.find((game) => game.coverUrl);
  test.skip(!coveredGame, "The current fixture has no published cover image.");

  await page.goto("/library");
  const cover = page.getByRole("img", { name: `${coveredGame!.title} 封面` }).first();
  await expect(cover).toHaveAttribute("src", coveredGame!.coverUrl!);
  await expect.poll(() => cover.evaluate((image: HTMLImageElement) => image.naturalWidth)).toBeGreaterThan(0);
});

test("HTML CSP uses a fresh nonce and only development enables unsafe-eval", async ({ page }) => {
  const response = await page.goto("/");
  const csp = response?.headers()["content-security-policy"] ?? "";
  expect(csp).toContain("'nonce-");
  expect(csp).toContain("'wasm-unsafe-eval'");
  if (process.env.RETROM_E2E_PRODUCTION === "1") {expect(csp).not.toContain("'unsafe-eval'");}
  else {expect(csp).toContain("'unsafe-eval'");}
});

test("one click creates a capability launch and advances real emulator frames", async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  test.skip(!["chrome-1280", "chrome-4k-150"].includes(testInfo.project.name), "The real core smoke covers the minimum desktop and physical 4K at 150% scaling.");
  const saveWrites: string[] = [];
  page.on("request", (request) => {
    const pathname = new URL(request.url()).pathname;
    if (request.method() === "POST" && /\/runtime\/launches\/[^/]+\/save-states$/.test(pathname)) {saveWrites.push(pathname);}
  });
  const games = await page.request.get("/api/v1/games");
  const payload = await games.json() as { items: Array<{ gameId: string; title: string }> };
  const game = payload.items.find((item) => item.title === "Sudoku");
  test.skip(!game, "The launchable acceptance fixture has not been imported.");
  await page.goto(`/games/${game!.gameId}`);
  await page.getByRole("button", { name: /^(开始游戏|重新开始游戏)$/ }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/, { timeout: 10_000 });
  const player = page.frameLocator("iframe.player-frame");
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
  await page.mouse.move(20, 20);
  await expect(page.locator(".player-game-meta")).toContainText("Sudoku");
  await page.getByRole("button", { name: "返回并退出游戏" }).click();
  const exitDialog = page.getByRole("alertdialog", { name: "退出游戏？" });
  await expect(exitDialog).toBeVisible();
  const saveResponsePromise = page.waitForResponse((response) => response.request().method() === "POST" && /\/runtime\/launches\/[^/]+\/save-states$/.test(response.url()));
  await exitDialog.getByRole("button", { name: "创建存档" }).click();
  const saveResponse = await saveResponsePromise;
  expect(saveResponse.status()).toBe(201);
  const pausedForSave = await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0);
  await page.waitForTimeout(350);
  expect(await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0)).toBeLessThanOrEqual(pausedForSave + 1);
  await expect(page.getByText("手动存档和截图已保存")).toBeVisible({ timeout: 20_000 });
  await expect(exitDialog.getByRole("button", { name: "已创建存档" })).toBeDisabled();
  await expect(exitDialog).toContainText("退出前存档已创建，可以安全退出。");
  await exitDialog.getByRole("button", { name: "取消" }).click();
  await expect(exitDialog).toBeHidden();
  const saves = await page.request.get("/api/v1/saves?limit=100");
  expect(saves.ok()).toBe(true);
  const savePayload = await saves.json() as { items: Array<{ saveStateId: string; gameId: string; name: string; screenshotUrl: string }> };
  const createdSave = savePayload.items.find((save) => save.gameId === game!.gameId && save.name.startsWith("手动存档"));
  expect(createdSave).toBeTruthy();
  const screenshotStats = await page.evaluate(async (screenshotUrl) => {
    const response = await fetch(screenshotUrl, { credentials: "same-origin", cache: "no-store" });
    if (!response.ok) {throw new Error(`save screenshot request failed: ${response.status}`);}
    const bitmap = await createImageBitmap(await response.blob());
    const canvas = document.createElement("canvas");
    canvas.width = 64;
    canvas.height = 64;
    const context = canvas.getContext("2d", { alpha: false });
    if (!context) {throw new Error("2D canvas unavailable");}
    context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    bitmap.close();
    const pixels = context.getImageData(0, 0, canvas.width, canvas.height).data;
    let brightPixels = 0;
    let minimum = 255;
    let maximum = 0;
    for (let index = 0; index < pixels.length; index += 4) {
      const luminance = (pixels[index] + pixels[index + 1] + pixels[index + 2]) / 3;
      if (luminance > 8) {brightPixels += 1;}
      minimum = Math.min(minimum, luminance);
      maximum = Math.max(maximum, luminance);
    }
    return { brightRatio: brightPixels / (pixels.length / 4), luminanceRange: maximum - minimum };
  }, createdSave!.screenshotUrl);
  expect(screenshotStats.brightRatio).toBeGreaterThan(0.02);
  expect(screenshotStats.luminanceRange).toBeGreaterThan(16);

  await page.getByRole("button", { name: "继续游戏" }).click();
  await expect.poll(async () => playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0), { timeout: 10_000 }).toBeGreaterThan(pausedForSave + 5);
  await page.mouse.move(20, 20);
  await page.getByRole("button", { name: "更多操作" }).click();
  await page.getByRole("menuitem", { name: "模拟器设置" }).click();
  const emulatorToolbar = page.getByRole("region", { name: "模拟器设置工具栏" });
  await expect(emulatorToolbar).toBeVisible();
  await expect(nativeMenu).toBeHidden();
  await expect(emulatorToolbar.getByRole("button", { name: /退出/ })).toHaveCount(0);
  const renderingMode = emulatorToolbar.getByRole("combobox", { name: "画面模式" });
  await expect(renderingMode).toHaveValue("pixel");
  await expect.poll(() => canvas.evaluate((element) => getComputedStyle(element).imageRendering)).toBe("pixelated");
  await renderingMode.selectOption("clear");
  await expect.poll(() => playerFrame!.evaluate(() => window.EJS_emulator?.allSettings?.shader)).toBe("retrom-sharp-bilinear");
  await renderingMode.selectOption("sharpen");
  await expect.poll(() => playerFrame!.evaluate(() => window.EJS_emulator?.allSettings?.shader)).toBe("retrom-adaptive-sharpen");
  await renderingMode.selectOption("original");
  await expect.poll(() => playerFrame!.evaluate(() => window.EJS_emulator?.allSettings?.shader)).toBe("disabled");
  await expect.poll(() => canvas.evaluate((element) => getComputedStyle(element).imageRendering)).toBe("auto");
  await renderingMode.selectOption("pixel");
  await expect.poll(() => playerFrame!.evaluate(() => window.EJS_emulator?.allSettings?.shader)).toBe("disabled");
  const renderingEvidence = await page.screenshot({ path: testInfo.outputPath("player-pixel-rendering.png") });
  expect(await screenshotCenterBrightRatio(page, renderingEvidence)).toBeGreaterThan(0.02);
  if (testInfo.project.name === "chrome-4k-150") {
    expect({ width: renderingEvidence.readUInt32BE(16), height: renderingEvidence.readUInt32BE(20) }).toEqual({ width: 3840, height: 2160 });
  }
  await emulatorToolbar.getByRole("button", { name: "Core 设置" }).click();
  await expect(player.getByRole("button", { name: /Backend Core Options|Core Options|核心选项|核心设置/ })).toBeVisible();
  await emulatorToolbar.getByRole("button", { name: "显示" }).click();
  await expect(player.getByRole("button", { name: /Graphics Settings|图形设置|显示设置/ })).toBeVisible();
  await expect(player.getByRole("button", { name: /Backend Core Options|Core Options|核心选项|核心设置/ })).toBeHidden();
  await expect(player.locator(".ejs_settings_main_bar").filter({ hasText: /Shaders|着色器/ })).toBeVisible();
  const pausedAt = await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0);
  await page.waitForTimeout(350);
  expect(await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0)).toBeLessThanOrEqual(pausedAt + 1);
  await emulatorToolbar.getByRole("button", { name: "收起" }).click();
  await expect(emulatorToolbar).toBeHidden();
  await expect(nativeMenu).toBeHidden();
  await page.getByRole("button", { name: "继续游戏" }).click();
  await expect.poll(async () => playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0), { timeout: 10_000 }).toBeGreaterThan(pausedAt + 5);
  await page.mouse.move(20, 1);
  await page.getByRole("button", { name: "返回并退出游戏" }).click();
  const directExitDialog = page.getByRole("alertdialog", { name: "退出游戏？" });
  await expect(directExitDialog).toContainText("只有点击“创建存档”才会保存当前位置");
  await directExitDialog.getByRole("button", { name: "退出游戏" }).click();
  await expect(page).toHaveURL(new RegExp(`/games/${game!.gameId}$`));
  expect(saveWrites).toHaveLength(1);
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
    for (const heading of ["发布信息", "媒体", "游戏文件与运行环境", "管理操作"]) {await expect(page.getByRole("heading", { name: heading })).toBeVisible();}
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
  await expect(page.getByRole("region", { name: "游戏存档" })).toBeVisible();
  expect(await page.locator(".game-detail-save-card").count()).toBeLessThanOrEqual(3);
  const layout = await page.evaluate(() => ({
    overflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
    savesTop: document.querySelector(".game-detail-saves")?.getBoundingClientRect().top ?? Number.POSITIVE_INFINITY,
    savesBottom: document.querySelector(".game-detail-saves")?.getBoundingClientRect().bottom ?? Number.POSITIVE_INFINITY,
    viewportHeight: document.documentElement.clientHeight,
    heroHeight: document.querySelector(".game-detail-hero")?.getBoundingClientRect().height ?? Number.POSITIVE_INFINITY,
    launchTopGap: (() => {
      const hero = document.querySelector(".game-detail-hero")?.getBoundingClientRect();
      const launch = document.querySelector(".launch-panel")?.getBoundingClientRect();
      return hero && launch ? launch.top - hero.top : Number.POSITIVE_INFINITY;
    })(),
    launchBottomGap: (() => {
      const hero = document.querySelector(".game-detail-hero")?.getBoundingClientRect();
      const launch = document.querySelector(".launch-panel")?.getBoundingClientRect();
      return hero && launch ? hero.bottom - launch.bottom : Number.POSITIVE_INFINITY;
    })(),
  }));
  expect(layout.overflow).toBe(false);
  if (testInfo.project.name === "chrome-1280") {expect(layout.savesTop).toBeLessThan(layout.viewportHeight);}
  else {
    expect(layout.savesBottom).toBeLessThanOrEqual(layout.viewportHeight);
    expect(layout.heroHeight).toBeLessThanOrEqual(415);
    expect(Math.max(layout.launchTopGap, layout.launchBottomGap)).toBeLessThanOrEqual(65);
    const typography = await page.evaluate(() => {
      const read = (selector: string) => {
        const element = document.querySelector(selector);
        const style = element ? getComputedStyle(element) : null;
        return { size: Number.parseFloat(style?.fontSize ?? "0"), weight: Number.parseInt(style?.fontWeight ?? "0", 10) };
      };
      return { title: read(".game-detail-main h1"), description: read(".game-detail-description") };
    });
    expect(typography.title.size).toBeGreaterThanOrEqual(36);
    expect(typography.title.weight).toBeGreaterThanOrEqual(700);
    expect(typography.description.size).toBeGreaterThanOrEqual(15);
  }
  const saveCards = await page.locator(".game-detail-save-card").evaluateAll((cards) => cards.map((card) => {
    const cardBox = card.getBoundingClientRect();
    const mediaBox = card.querySelector(".game-detail-save-media")?.getBoundingClientRect();
    const bodyBox = card.querySelector(".game-detail-save-body")?.getBoundingClientRect();
    return { x: cardBox.x, y: cardBox.y, width: cardBox.width, mediaWidth: mediaBox?.width ?? 0, mediaBottom: mediaBox?.bottom ?? 0, bodyTop: bodyBox?.top ?? 0 };
  }));
  if (testInfo.project.name !== "chrome-1280" && saveCards.length === 3) {
    expect(Math.max(...saveCards.map((card) => card.y)) - Math.min(...saveCards.map((card) => card.y))).toBeLessThanOrEqual(1);
    expect(saveCards[0].x).toBeLessThan(saveCards[1].x);
    expect(saveCards[1].x).toBeLessThan(saveCards[2].x);
    for (const card of saveCards) {
      expect(card.mediaWidth / card.width).toBeGreaterThanOrEqual(.98);
      expect(card.bodyTop).toBeGreaterThanOrEqual(card.mediaBottom - 1);
    }
    const firstSaveCard = page.locator(".game-detail-save-card").first();
    await expect(firstSaveCard.getByText("最近存档", { exact: true })).toBeVisible();
    await expect(firstSaveCard.locator(".game-detail-save-fact-row > span")).toHaveCount(3);
    for (const label of ["保存位置", "运行核心", "当时已游玩"]) {await expect(firstSaveCard.getByText(label, { exact: true })).toBeVisible();}
    const cardAndAction = await firstSaveCard.evaluate((card) => {
      const body = card.querySelector(".game-detail-save-body")?.getBoundingClientRect();
      const action = card.querySelector(".game-detail-save-body .button")?.getBoundingClientRect();
      const titleStyle = getComputedStyle(card.querySelector(".game-detail-save-title-line strong")!);
      const factStyle = getComputedStyle(card.querySelector(".game-detail-save-fact-row b")!);
      const actionStyle = getComputedStyle(card.querySelector(".game-detail-save-body .button")!);
      return {
        bodyWidth: body?.width ?? 0,
        actionWidth: action?.width ?? 0,
        titleSize: Number.parseFloat(titleStyle.fontSize),
        titleWeight: Number.parseInt(titleStyle.fontWeight, 10),
        factSize: Number.parseFloat(factStyle.fontSize),
        factWeight: Number.parseInt(factStyle.fontWeight, 10),
        actionSize: Number.parseFloat(actionStyle.fontSize),
        actionWeight: Number.parseInt(actionStyle.fontWeight, 10),
      };
    });
    expect(cardAndAction.actionWidth / cardAndAction.bodyWidth).toBeGreaterThanOrEqual(.94);
    expect(cardAndAction.titleSize).toBeGreaterThanOrEqual(14);
    expect(cardAndAction.titleWeight).toBeGreaterThanOrEqual(700);
    expect(cardAndAction.factSize).toBeGreaterThanOrEqual(12);
    expect(cardAndAction.factWeight).toBeGreaterThanOrEqual(700);
    expect(cardAndAction.actionSize).toBeGreaterThanOrEqual(11);
    expect(cardAndAction.actionWeight).toBeGreaterThanOrEqual(700);
  }

  const runtimeButton = page.getByRole("button", { name: /更换/ });
  await runtimeButton.click();
  await expect(page.getByRole("alertdialog", { name: "更换运行方式" })).toBeVisible();
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await expect(page.getByRole("alertdialog", { name: "更换运行方式" })).toHaveCount(0);

  const expectedSaveCount = saves.items.filter((save) => save.gameId === preferredGameId).length;
  if (expectedSaveCount > 0) {
    await page.getByRole("button", { name: "查看全部存档" }).click();
    const drawer = page.getByRole("dialog", { name: "全部存档" });
    const drawerElement = page.locator(".game-detail-save-drawer");
    await expect(drawer).toBeVisible();
    await expect(drawer.locator(".game-detail-drawer-row")).toHaveCount(expectedSaveCount);
    await expect(drawer.getByRole("button", { name: "▶ 继续" }).first()).toBeVisible();
    const drawerRowLayout = await drawer.locator(".game-detail-drawer-row").first().evaluate((row) => {
      const rowBox = row.getBoundingClientRect();
      const shotBox = row.querySelector(".game-detail-drawer-shot")?.getBoundingClientRect();
      const actionBox = row.querySelector(".game-detail-drawer-row > .button")?.getBoundingClientRect();
      const actionStyle = actionBox ? getComputedStyle(row.querySelector(".game-detail-drawer-row > .button")!) : null;
      const timeStyle = getComputedStyle(row.querySelector("time")!);
      return {
        height: rowBox.height,
        shotWidth: shotBox?.width ?? 0,
        actionBottomGap: actionBox ? rowBox.bottom - actionBox.bottom : Number.POSITIVE_INFINITY,
        actionColor: actionStyle?.color ?? "",
        actionBackground: actionStyle?.backgroundColor ?? "",
        actionSize: Number.parseFloat(actionStyle?.fontSize ?? "0"),
        actionWeight: Number.parseInt(actionStyle?.fontWeight ?? "0", 10),
        timeSize: Number.parseFloat(timeStyle.fontSize),
        timeWeight: Number.parseInt(timeStyle.fontWeight, 10),
      };
    });
    expect(drawerRowLayout.height).toBeGreaterThanOrEqual(138);
    expect(drawerRowLayout.shotWidth).toBeGreaterThanOrEqual(190);
    expect(drawerRowLayout.actionBottomGap).toBeLessThanOrEqual(13);
    expect(drawerRowLayout.actionColor).toBe("rgb(255, 255, 255)");
    expect(drawerRowLayout.actionBackground).toBe("rgb(98, 80, 210)");
    expect(drawerRowLayout.actionSize).toBeGreaterThanOrEqual(11);
    expect(drawerRowLayout.actionWeight).toBeGreaterThanOrEqual(700);
    expect(drawerRowLayout.timeSize).toBeGreaterThanOrEqual(14);
    expect(drawerRowLayout.timeWeight).toBeGreaterThanOrEqual(700);
    await page.screenshot({ path: testInfo.outputPath("game-detail-save-drawer.png"), fullPage: true });
    await drawer.getByRole("button", { name: /预览.*存档截图/ }).first().click();
    await expect(page.getByRole("dialog", { name: "存档截图预览" })).toBeVisible();
    await page.getByRole("button", { name: "关闭" }).click();
    await page.getByRole("button", { name: "关闭全部存档" }).click();
    await expect(drawer).toBeHidden();
    await expect(drawerElement).toHaveCSS("visibility", "hidden");
    await expect(page.locator(".game-detail-drawer-backdrop")).toHaveCSS("opacity", "0");
    await expect(page).toHaveURL(new RegExp(`/games/${preferredGameId}$`));
  }
  await page.screenshot({ path: testInfo.outputPath("game-detail-one-screen.png"), fullPage: true });
});

test("BIOS and save controls are labeled and keyboard reachable", async ({ page }, testInfo) => {
  await page.goto("/admin/bios?scope=FULL_CATALOG");
  await expect(page.getByRole("heading", { name: "运行依赖" })).toBeVisible();
  const biosTab = page.getByRole("tab", { name: "BIOS 文件" });
  await expect(biosTab).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("tabpanel", { name: "BIOS 文件" })
    .getByRole("searchbox", { name: "搜索 BIOS 文件" })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  await page.goto("/saves?availability=ALL");
  await expect(page.getByRole("heading", { name: "我的存档" })).toBeVisible();
  const rename = page.getByRole("button", { name: /编辑存档.*的名称/ }).first();
  if (await rename.count()) {await expect(rename).toBeVisible();}
  await page.keyboard.press("Tab");
  expect(await page.evaluate(() => document.activeElement?.tagName !== "BODY")).toBe(true);
  await page.screenshot({ path: testInfo.outputPath("bios-saves.png"), fullPage: true });
});
