import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { currentEmulatorBrightRatio, evidencePath, noPageOverflow } from "./acceptance-support";
import { verifyCompactFeaturedHome, verifyMobileSavedFeaturedHome } from "./acceptance-user-layout";

export function registerRuntimeAcceptanceTests(): void {
  test.describe("post-publication runtime acceptance", () => {
    test.beforeEach(async ({ page }, testInfo) => {
      test.skip(testInfo.project.name !== "chrome-1280", "此状态型 Case 只消费一次共享验收夹具");
      const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
      const response = await page.request.post("/api/v1/auth/login", { data: { username: "test", password: "test" }, headers: { Origin: origin } });
      expect(response.ok()).toBe(true);
    });
    registerRun002();
    registerRun003();
    registerRun004();
    registerRun006();
    registerRun007();
    registerSave002();
    registerLanUpload();
  });
}

function registerRun002(): void {
  test("ACC-RUN-002 one click requests fullscreen before launch and auto-starts the locked runtime", async ({ page }, testInfo) => {
    test.setTimeout(180_000);
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
    const libraryResponse = await page.goto("/library");
    expect(libraryResponse?.headers()["content-security-policy"])
      .toMatch(/frame-src 'self' http:\/\/\*\.rpg\.localhost:\d+/);
    await page.locator(".library-game-card").filter({ hasText: "Sudoku" }).getByRole("link").first().click();
    const sourceDocumentIdentity = await page.evaluate(() => {
      const scope = window as Window & { __retromDocumentIdentity?: string };
      scope.__retromDocumentIdentity ??= crypto.randomUUID();
      return scope.__retromDocumentIdentity;
    });
    const configResponse = page.waitForResponse((response) => /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
    await page.getByRole("button", { name: "开始游戏" }).click();
    await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
    expect(await page.evaluate(() => (window as Window & { __retromDocumentIdentity?: string }).__retromDocumentIdentity))
      .toBe(sourceDocumentIdentity);
    const configuration = await (await configResponse).json() as { emulatorGameId: number; gameName: string; gameTitle: string; coreName: string; platformName: string; gameUrl: string; loaderUrl: string; runtimePathOverrides: Record<string, string>; playerAdapterId: string; emulatorjsVersion: string };
    expect(Number.isSafeInteger(configuration.emulatorGameId) && configuration.emulatorGameId > 0).toBe(true);
    expect(configuration.gameName).toBe(`retrom-${configuration.emulatorGameId}`);
    expect(configuration.gameTitle).toBe("Sudoku");
    expect(configuration.coreName).toBe("mGBA");
    expect(configuration.platformName).toBe("Game Boy Advance");
    expect(configuration.playerAdapterId).toBe("ejs-4.2.3-v3");
    expect(configuration.emulatorjsVersion).toBe("4.2.3");
    expect(configuration.gameUrl).not.toMatch(/(?:blob:|file:|\/home\/)/);
    await expect(page.locator(".player-shell")).toBeVisible();
    await expect(page.getByRole("button", { name: "开始游戏" })).toHaveCount(0);
    await expect(page.locator(".player-loading")).toBeHidden({ timeout: 30_000 });
    const playerCanvas = page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas");
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
    const playerFrame = page.frames().find((frame) => frame !== page.mainFrame());
    expect(playerFrame).toBeTruthy();
    const controls = await playerFrame!.evaluate(() => {
      const runtime = window.EJS_emulator as typeof window.EJS_emulator & {
        controls?: Record<number, Record<number, { value?: number; value2?: string }>>;
      };
      return runtime?.controls;
    });
    expect(controls?.[0]).toMatchObject({
      0: { value: 74, value2: "BUTTON_2" },
      2: { value: 53, value2: "SELECT" },
      3: { value: 49, value2: "START" },
      4: { value: 87, value2: "DPAD_UP" },
      5: { value: 83, value2: "DPAD_DOWN" },
      6: { value: 65, value2: "DPAD_LEFT" },
      7: { value: 68, value2: "DPAD_RIGHT" },
      8: { value: 75, value2: "BUTTON_1" },
      24: { value: 0 }, 25: { value: 0 }, 26: { value: 0 }, 27: { value: 0 }, 28: { value: 0 }, 29: { value: 0 },
    });
    expect(controls?.[1]).toMatchObject({
      0: { value: 97 }, 2: { value: 0 }, 3: { value: 50 }, 4: { value: 38 }, 5: { value: 40 },
      6: { value: 37 }, 7: { value: 39 }, 8: { value: 98 },
    });
    const frameBeforePause = await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0);
    await playerFrame!.locator("body").press("p");
    await expect(page.locator(".player-shell")).toHaveClass(/is-paused/);
    const pausedFrame = await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0);
    expect(pausedFrame).toBeGreaterThanOrEqual(frameBeforePause);
    await page.waitForTimeout(350);
    expect(await playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0)).toBeLessThanOrEqual(pausedFrame + 1);
    await playerFrame!.locator("body").press("p");
    await expect(page.locator(".player-shell")).not.toHaveClass(/is-paused/);
    await expect.poll(() => playerFrame!.evaluate(() => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0), { timeout: 10_000 }).toBeGreaterThan(frameBeforePause + 5);
    await page.mouse.move(20, 20);
    const debugButton = page.getByRole("button", { name: "调试信息" });
    await expect(debugButton).toBeVisible();
    await debugButton.click();
    const debugPanel = page.getByRole("complementary", { name: "运行调试信息" });
    await expect(debugPanel).toBeVisible();
    await expect(debugPanel.getByText(/^\d+\.\d FPS$/)).toBeVisible({ timeout: 5_000 });
    await expect(debugPanel.getByText("mGBA", { exact: true })).toBeVisible();
    await expect(debugPanel.getByText("4.2.3", { exact: true })).toBeVisible();
    await expect(debugPanel.getByText("ejs-4.2.3-v3", { exact: true })).toBeVisible();
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
    await page.locator(".player-pause-overlay").click();
    await expect(page.locator(".player-shell")).not.toHaveClass(/is-paused/);
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
    for (const artifactURL of Object.values(configuration.runtimePathOverrides)) {expect(requests.some((url) => url.endsWith(artifactURL))).toBe(true);}
    expect(requests.some((url) => /\/localization\/zh-CN\.json$/.test(url))).toBe(true);
    const applicationHost = new URL(page.url()).host;
    expect(requests.some((url) => {
      try {
        const parsed = new URL(url);
        if (parsed.protocol === "about:") {return false;}
        if (parsed.protocol === "blob:") {return new URL(url.slice("blob:".length)).host !== applicationHost;}
        return parsed.host !== applicationHost;
      } catch { return false; }
    })).toBe(false);
    await page.screenshot({ path: evidencePath(testInfo, "one-click-auto-start.png"), fullPage: true });
  });
}

function registerRun003(): void {
  test("ACC-RUN-003 fullscreen refusal and launch deep-link credentials remain recoverable", async ({ page, browser }, testInfo) => {
    test.setTimeout(180_000);
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
}

function registerRun004(): void {
  test("ACC-RUN-004 BIOS blockers stop launch while hash warnings auto-start", async ({ page }, testInfo) => {
    test.setTimeout(180_000);
    await page.addInitScript(() => {
      const record = (event: string) => sessionStorage.setItem(`retrom:${event}`, "true");
      Object.defineProperty(Element.prototype, "requestFullscreen", { configurable: true, value: () => { record("fullscreen-requested"); return Promise.resolve(); } });
      Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => document.documentElement });
      Object.defineProperty(document, "exitFullscreen", { configurable: true, value: () => { record("fullscreen-exited"); return Promise.resolve(); } });
    });
    await page.goto("/admin/bios?scope=FULL_CATALOG&q=gba_bios.bin");
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
    await page.mouse.move(20, 20);
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
}

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
    if (new URL(request.url()).pathname.startsWith("/runtime/")) {runtimeRequests.push(request.url());}
  });
  page.on("requestfailed", (request) => {
    const pathname = new URL(request.url()).pathname;
    const errorText = request.failure()?.errorText ?? "unknown";
    const canceledPlayerProbe = errorText === "net::ERR_ABORTED"
      && /\/runtime\/launches\/[^/]+\/config$/.test(pathname);
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
    playerAdapterId: "ejs-4.2.3-v3",
  });
  expect(configuration.gameUrl).toMatch(/\/runtime\/content\/game\/[0-9a-f]{64}\/pacman\.zip$/);
  expect(configuration.parentUrl).toMatch(/\/runtime\/content\/parent\/[0-9a-f]{64}\/bundle\.zip$/);
  expect(configuration.biosUrl).toMatch(/\/runtime\/content\/bios\/[0-9a-f]{64}\/bundle\.zip$/);
  expect(configuration.runtimePathOverrides).toEqual(expectation.runtimePathOverrides);

  await expect(page.locator(".player-loading")).toBeHidden({ timeout: 60_000 });
  const canvas = page.frameLocator("iframe.player-frame").locator("canvas");
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
    if (!(element instanceof HTMLCanvasElement)) {return { width: 0, height: 0 };}
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
  expect(configuration.parentUrl).toMatch(/\/runtime\/content\/parent\/[0-9a-f]{64}\/bundle\.zip$/);
  expect(configuration.biosUrl).toMatch(/\/runtime\/content\/bios\/[0-9a-f]{64}\/bundle\.zip$/);
  expect(configuration.warnings).toContain("REVIEW_SCREENSHOT_OVERRIDE");
  await expect(page.locator(".player-loading")).toBeHidden({ timeout: 60_000 });
  await expect(page.frameLocator("iframe.player-frame").locator("canvas")).toBeVisible({ timeout: 10_000 });
  await page.screenshot({ path: evidencePath(testInfo, expectation.schemaV2ScreenshotName), fullPage: true });
}

function registerRun006(): void {
  test("ACC-RUN-006 public MAME 2003 split set locks test-only built-in DAT, Parent and BIOS, then executes frames", async ({ page }, testInfo) => {
    test.setTimeout(300_000);
    const platformInstanceId = process.env.RETROM_MAME2003_PLATFORM_INSTANCE_ID;
    expect(platformInstanceId, "MAME 2003 acceptance directory ID").toBeTruthy();
    const expectation: PublicArcadeSmokeExpectation = {
      platformInstanceId: platformInstanceId!,
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
}

function registerRun007(): void {
  test("ACC-RUN-007 public FBNeo split set locks test-only built-in DAT, Parent and BIOS, then executes frames", async ({ page }, testInfo) => {
    test.setTimeout(300_000);
    const platformInstanceId = process.env.RETROM_FBNEO_PLATFORM_INSTANCE_ID;
    expect(platformInstanceId, "FBNeo acceptance directory ID").toBeTruthy();
    const expectation: PublicArcadeSmokeExpectation = {
      platformInstanceId: platformInstanceId!,
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
}

function registerSave002(): void {
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
    await page.mouse.move(640, 1);
    await expect(page.locator(".player-toolbar")).toHaveCSS("opacity", "1");
    const saveResponse = page.waitForResponse((response) =>
      /\/runtime\/launches\/[^/]+\/save-states$/.test(response.url()) && response.request().method() === "POST");
    await page.locator(".player-save-button").click();
    await expect(page.locator(".player-save-upload-progress")).toBeVisible({ timeout: 10_000 });
    const savedResponse = await saveResponse;
    expect(savedResponse.status()).toBe(201);
    const saved = await savedResponse.json() as { saveStateId: string };
    await expect(page.locator(".player-save-upload-progress")).toBeHidden({ timeout: 5_000 });
  
    await page.goto(detailURL);
    const freshConfigResponse = page.waitForResponse((response) =>
      /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
    await page.getByRole("button", { name: "重新开始游戏" }).click();
    const freshConfig = await (await freshConfigResponse).json() as { stateUrl: string | null };
    expect(freshConfig).toMatchObject({ stateUrl: null });
    await expect(page.locator(".player-loading")).toBeHidden({ timeout: 60_000 });
  
    await page.goto(detailURL);
    const detailResumeConfigResponse = page.waitForResponse((response) =>
      /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
    await page.getByRole("button", { name: "从存档继续" }).click();
    await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
    const detailResumeConfig = await (await detailResumeConfigResponse).json() as { stateUrl: string | null };
    expect(detailResumeConfig.stateUrl).toMatch(/\/runtime\/launches\/[^/]+\/state$/);
    await expect(page.locator(".player-loading")).toBeHidden({ timeout: 60_000 });
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
    const savesResumeConfig = await (await resumedConfigResponse).json() as { stateUrl: string | null };
    expect(savesResumeConfig.stateUrl).toMatch(/\/runtime\/launches\/[^/]+\/state$/);
    await expect(page.locator(".player-loading")).toBeHidden({ timeout: 60_000 });
    await page.mouse.move(640, 1);
    await expect(page.locator(".player-toolbar")).toHaveCSS("opacity", "1");
    const latestSaveResponse = page.waitForResponse((response) =>
      /\/runtime\/launches\/[^/]+\/save-states$/.test(response.url()) && response.request().method() === "POST");
    await page.locator(".player-save-button").click();
    expect((await latestSaveResponse).status()).toBe(201);
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
    await verifyMobileSavedFeaturedHome(page, testInfo);
    await verifyCompactFeaturedHome(page, testInfo);
    const homeResumeConfigResponse = page.waitForResponse((response) =>
      /\/runtime\/launches\/[^/]+\/config$/.test(response.url()) && response.status() === 200);
    await page.getByRole("button", { name: "继续游玩" }).click();
    await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
    const homeResumeConfig = await (await homeResumeConfigResponse).json() as { stateUrl: string | null };
    expect(homeResumeConfig.stateUrl).toMatch(/\/runtime\/launches\/[^/]+\/state$/);
    await expect(page.locator(".player-loading")).toBeHidden({ timeout: 60_000 });
  
    const authContext = await page.request.get("/api/v1/auth/context");
    const csrfToken = (await authContext.json() as { csrfToken: string }).csrfToken;
    const mismatch = await page.request.post("/api/v1/launches", {
      headers: { Origin: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000", "X-Retrom-Csrf": csrfToken, "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() },
      data: { gameId, coreId: "gambatte", saveStateId: saved.saveStateId, dosEntry: null, returnTo: "/saves", clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true } },
    });
    expect(mismatch.status()).toBe(422);
    expect((await mismatch.json() as { error: { code: string } }).error.code).toBe("LAUNCH_BLOCKED");
    await page.screenshot({ path: evidencePath(testInfo, "three-save-resume-entry-points.png"), fullPage: true });
  });
}

function registerLanUpload(): void {
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
    await page.getByRole("button", { name: "查看任务进度" }).click();
    await expect(page).toHaveURL(/\/admin\/imports\/tasks$/);
  });
}
