import { expect, test, type Page } from "@playwright/test";
import axe from "axe-core";
import { expectMobileLocalDraftNotice } from "./mobile-local-draft";
import { evidencePath, expectNoTextArrowsInInteractiveControls } from "./acceptance-support";

declare global {
  interface Window {
    axe: {
    run: (root: Document, options: object) => Promise<{
      violations: Array<{ id: string; impact: string | null; nodes: unknown[] }>;
    }>;
    };
  }
}

const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";

async function login(page: Page) {
  const response = await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" }, headers: { Origin: origin },
  });
  expect(response.ok()).toBe(true);
  return response.json() as Promise<{ csrfToken: string }>;
}

async function expectNoDocumentOverflow(page: Page) {
  const sizes = await page.evaluate(() => ({
    client: document.documentElement.clientWidth,
    scroll: document.documentElement.scrollWidth,
  }));
  expect(sizes.scroll, `document width ${sizes.scroll}, viewport ${sizes.client}`).toBeLessThanOrEqual(sizes.client + 1);
}

async function expectMinimumTargets(page: Page, selector: string) {
  const undersized = await page.locator(selector).evaluateAll((elements) => elements
    .filter((element) => {
      const style = getComputedStyle(element);
      return style.display !== "none" && style.visibility !== "hidden";
    })
    .map((element) => {
      const rect = element.getBoundingClientRect();
      return { label: element.getAttribute("aria-label") ?? element.textContent?.trim(), width: rect.width, height: rect.height };
    })
    .filter((item) => item.width < 44 || item.height < 44));
  expect(undersized).toEqual([]);
}

async function expectLibraryFilterAlignment(page: Page) {
  const button = page.getByRole("button", { name: /筛选与排序/ });
  const bounds = await button.boundingBox();
  const icon = await button.locator("svg").boundingBox();
  const search = await page.locator(".library-search").boundingBox();
  expect(bounds).not.toBeNull();
  expect(icon).not.toBeNull();
  expect(search).not.toBeNull();
  expect(bounds!.width).toBe(bounds!.height);
  expect(bounds!.height).toBe(search!.height);
  expect(Math.abs(icon!.x + icon!.width / 2 - bounds!.x - bounds!.width / 2)).toBeLessThan(1);
  expect(Math.abs(icon!.y + icon!.height / 2 - bounds!.y - bounds!.height / 2)).toBeLessThan(1);
}

async function expectSearchFocus(page: Page, selector: string) {
  const field = page.locator(selector);
  const input = field.getByRole("searchbox");
  await input.focus();
  await expect(input).toBeFocused();
  const focus = await field.evaluate((element) => {
    const input = element.querySelector("input")!;
    const style = getComputedStyle(element);
    const bounds = element.getBoundingClientRect();
    const clips = [];
    for (let parent = element.parentElement; parent; parent = parent.parentElement) {
      const parentStyle = getComputedStyle(parent);
      const parentBounds = parent.getBoundingClientRect();
      if (parentStyle.overflowX === "hidden" && (bounds.left - 3 < parentBounds.left || bounds.right + 3 > parentBounds.right)) { clips.push(parent.className); }
      if (parentStyle.overflowY === "hidden" && (bounds.top - 3 < parentBounds.top || bounds.bottom + 3 > parentBounds.bottom)) { clips.push(parent.className); }
    }
    return { inputOutline: getComputedStyle(input).outlineStyle, shadow: style.boxShadow, clips };
  });
  expect(focus.inputOutline).toBe("none");
  expect(focus.shadow).not.toBe("none");
  expect(focus.clips).toEqual([]);
  await input.blur();
}

async function expectHomeLaunchPlacement(page: Page) {
  for (const width of [390, 480, 600, 844]) {
    await page.setViewportSize({ width, height: width === 844 ? 390 : 844 });
    const card = page.locator(".phone-continue-card");
    const bounds = await card.boundingBox();
    const title = await card.locator("h2").boundingBox();
    const action = await card.getByRole("button").boundingBox();
    expect(bounds).not.toBeNull();
    expect(title).not.toBeNull();
    expect(action).not.toBeNull();
    expect(Math.abs(action!.x + action!.width - bounds!.x - bounds!.width)).toBeLessThan(1);
    expect(action!.height).toBeGreaterThanOrEqual(48);
    if (width >= 480) {
      expect(action!.x).toBeGreaterThanOrEqual(title!.x + title!.width + 12);
      expect(Math.abs(action!.y + action!.height / 2 - bounds!.y - bounds!.height / 2)).toBeLessThan(1);
    } else {
      expect(action!.y).toBeGreaterThan(title!.y + title!.height);
    }
    await expectNoDocumentOverflow(page);
  }
  await page.setViewportSize({ width: 390, height: 844 });
}

test.beforeEach(async ({ page }) => { await login(page); });

test("ACC-MOB-001 exact phone and tablet shell baselines have no page overflow", async ({ page }) => {
  const viewports = [
    { width: 320, height: 568 },
    { width: 360, height: 800 },
    { width: 390, height: 844 },
    { width: 412, height: 915 },
    { width: 844, height: 390 },
    { width: 768, height: 1024 },
    { width: 1024, height: 768 },
  ];
  for (const viewport of viewports) {
    const phone = viewport.width < 768 || (viewport.width < 1024 && viewport.height < 768);
    await page.setViewportSize(viewport);
    await page.goto("/library");
    await expect(page.getByRole("navigation", { name: phone ? "手机主导航" : "主要导航" }).or(page.getByRole("button", { name: "打开主要导航" })).first()).toBeVisible();
    await expectNoDocumentOverflow(page);
    if (phone) {
      const bottom = page.getByRole("navigation", { name: "手机主导航" });
      await expect(bottom).toBeVisible();
      await expect(bottom.getByRole("link", { name: "游戏库" })).toHaveAttribute("aria-current", "page");
      await expect(bottom.getByRole("link")).toHaveCount(3);
      await expectMinimumTargets(page, ".phone-bottom-nav > a, .phone-app-header > a");
      await expectLibraryFilterAlignment(page);
      const grid = page.locator(".library-game-grid");
      if (await grid.count()) {
        const columns = await grid.evaluate((element) => getComputedStyle(element).gridTemplateColumns.split(" ").length);
        expect(columns).toBe(viewport.width >= 480 ? 3 : 2);
      } else {
        await expect(page.getByRole("heading", { name: "游戏库还是空的" })).toBeVisible();
      }
    } else {
      await expect(page.getByRole("button", { name: "打开主要导航" })).toBeVisible();
    }
  }
});

test("ACC-MOB-002 user routes, filter sheet, active navigation and accessibility remain usable", async ({ page }) => {
  test.setTimeout(180_000);
  await page.setViewportSize({ width: 390, height: 844 });
  const routes = ["/", "/library", "/me", "/saves", "/favorites", "/recent", "/netplay", "/account"];
  for (const route of routes) {
    await page.goto(route);
    await expect(page.locator("main").first()).toBeVisible();
    await expectNoDocumentOverflow(page);
    await expectNoTextArrowsInInteractiveControls(page);
    if (route === "/" || route === "/library") {
      await expectSearchFocus(page, route === "/" ? ".phone-home-search" : ".library-search");
    }
    if (route === "/library") {
      const rail = page.locator(".library-platform-row");
      const indicator = page.locator(".phone-platform-scrollbar");
      await expect(indicator).toHaveCSS("opacity", "0");
      if (await rail.evaluate((element) => element.scrollWidth > element.clientWidth)) {
        await rail.evaluate((element) => element.scrollBy(120, 0));
        await expect(indicator).toHaveCSS("opacity", "1");
        await expect(indicator).toHaveCSS("opacity", "0");
      }
    }
    const personalOptions = route === "/favorites" ? "整理与排序" : route === "/saves" ? "筛选存档" : route === "/recent" ? "筛选与排序" : null;
    if (personalOptions) {
      const disclosure = page.locator(".phone-disclosure");
      if (route === "/recent") {
        const empty = page.getByRole("heading", { name: "还没有游玩记录" });
        await expect(disclosure.or(empty)).toBeVisible();
        if (await empty.isVisible()) {
          await expect(disclosure).toHaveCount(0);
          continue;
        }
      }
      await expect(disclosure).not.toHaveAttribute("open", "");
      await disclosure.getByText(personalOptions, { exact: true }).click();
      await expect(disclosure.getByRole("combobox").first()).toBeVisible();
      await expectNoDocumentOverflow(page);
      await disclosure.getByText(personalOptions, { exact: true }).click();
    }
    if (route === "/") {
      await expect(page.getByRole("button", { name: "进入沉浸模式" })).toHaveCount(0);
      await page.setViewportSize({ width: 844, height: 390 });
      await expect(page.getByRole("button", { name: "进入沉浸模式" })).toHaveCount(0);
      await page.setViewportSize({ width: 390, height: 844 });
      for (const poster of await page.locator(".phone-game-poster").all()) {
        const bounds = await poster.boundingBox();
        expect(bounds!.width / bounds!.height).toBeCloseTo(5 / 7, 2);
      }
    }
  }

  await page.goto("/library");
  const filterTrigger = page.getByRole("button", { name: /筛选与排序/ });
  await filterTrigger.click();
  const sheet = page.getByRole("dialog", { name: "筛选与排序" });
  await expect(sheet).toBeVisible();
  await sheet.getByRole("combobox", { name: "排列顺序" }).selectOption("TITLE_ASC");
  await sheet.getByRole("button", { name: "取消" }).click();
  await expect(filterTrigger).toBeFocused();
  await expect(page).not.toHaveURL(/sort=/);

  await page.getByRole("navigation", { name: "手机主导航" }).getByRole("link", { name: "我的" }).click();
  await expect(page).toHaveURL(/\/me$/);
  await expect(page.getByRole("link", { name: /最近游玩/ })).toBeVisible();
  await expect(page.locator('a[href^="/admin"]')).toHaveCount(0);
  await expectMinimumTargets(page, ".phone-profile-links > a, .phone-profile-links > button");
  await expectMobileLocalDraftNotice(page);

  await page.evaluate(axe.source);
  const serious = await page.evaluate(async () => {
    const result = await window.axe.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    return result.violations.filter((violation) => violation.impact === "serious" || violation.impact === "critical");
  });
  expect(serious).toEqual([]);
});

test("ACC-MOB-003 search, favorite, launch, save and home continue use the real product path", async ({ page }, testInfo) => {
  test.setTimeout(180_000);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await page.getByRole("searchbox", { name: "搜索游戏" }).fill("Sudoku");
  await page.getByRole("button", { name: "搜索", exact: true }).click();
  await expect(page).toHaveURL(/\/library\?q=Sudoku/);
  const card = page.locator(".library-game-card").filter({ hasText: "Sudoku" }).first();
  await expect(card).toBeVisible();
  const favorite = card.getByRole("button", { name: /^收藏“/ });
  if (await favorite.count()) {await favorite.click();}
  await expect(card.getByRole("button", { name: /^取消收藏“/ })).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: /筛选与排序/ }).click();
  const filters = page.getByRole("dialog", { name: "筛选与排序" });
  await filters.getByRole("combobox", { name: "排列顺序" }).selectOption("TITLE_ASC");
  await filters.getByRole("button", { name: "应用", exact: true }).click();
  await expect(page).toHaveURL(/sort=TITLE_ASC/);
  await expectLibraryFilterAlignment(page);
  await page.reload();
  await expect(page.getByRole("searchbox", { name: "搜索游戏" })).toHaveValue("Sudoku");
  await card.getByRole("link").first().click();
  await expect(page).toHaveURL(/\/games\/[0-9a-f-]+$/);
  const detailURL = page.url();
  await expect(page.locator(".phone-disclosure").first()).not.toHaveAttribute("open", "");
  await page.getByText("游戏简介", { exact: true }).click();
  await expect(page.locator(".game-detail-description")).toBeVisible();
  await page.evaluate(axe.source);
  const detailViolations = await page.evaluate(async () => {
    const result = await window.axe.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    return result.violations.filter((violation) => violation.impact === "serious" || violation.impact === "critical");
  });
  expect(detailViolations).toEqual([]);
  await page.getByRole("button", { name: "启动选项" }).click();
  const options = page.getByRole("dialog", { name: "启动选项" });
  await expect(options.getByRole("combobox", { name: "运行方式" })).toBeVisible();
  await expectNoDocumentOverflow(page);
  await page.screenshot({ path: evidencePath(testInfo, "phone-launch-options.png") });
  const launchRequest = page.waitForRequest((request) => request.method() === "POST" && /\/api\/v1\/launches$/.test(request.url()));
  await options.getByRole("button", { name: /^(开始游戏|从头开始)$/ }).click();
  expect((await launchRequest).postDataJSON()).toMatchObject({ saveStateId: null });
  await page.waitForURL(/\/play\/[0-9a-f-]+(?:\?|$)/, { timeout: 30_000 });
  await expect(page.getByRole("dialog", { name: "请横向握持设备开始游戏" })).toBeVisible();
  await page.setViewportSize({ width: 844, height: 390 });
  await expect(page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas")).toBeVisible({ timeout: 60_000 });
  const handle = page.getByRole("button", { name: /Player 控制栏/ });
  if (await handle.getAttribute("aria-pressed") !== "true") {await handle.tap();}
  await expect(handle).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByRole("button", { name: "返回并退出游戏" })).toBeInViewport();
  await page.getByRole("button", { name: "返回并退出游戏" }).tap();
  const exit = page.getByRole("alertdialog", { name: "退出游戏？" });
  const saveResponse = page.waitForResponse((response) => response.request().method() === "POST" && /\/runtime\/launches\/[^/]+\/save-states$/.test(response.url()));
  await exit.getByRole("button", { name: "创建存档", exact: true }).click();
  const saved = await saveResponse;
  expect(saved.status()).toBe(201);
  const save = await saved.json() as { saveStateId: string };
  expect(save.saveStateId).toBeTruthy();
  await expect(exit.getByRole("button", { name: "已创建存档" })).toBeDisabled();
  await exit.getByRole("button", { name: "退出游戏", exact: true }).click();
  await expect(page).toHaveURL(detailURL);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("navigation", { name: "手机主导航" }).getByRole("link", { name: "首页" }).click();
  await expect(page.getByRole("heading", { name: "继续游玩", exact: true })).toBeVisible();
  await expectHomeLaunchPlacement(page);
  await page.screenshot({ path: evidencePath(testInfo, "phone-home-continue.png"), fullPage: true });
  const resume = page.waitForRequest((request) => request.method() === "POST" && /\/api\/v1\/launches$/.test(request.url()));
  await page.getByRole("button", { name: "从存档继续", exact: true }).click();
  expect((await resume).postDataJSON()).toMatchObject({ saveStateId: save.saveStateId, returnTo: "/" });
  await page.setViewportSize({ width: 844, height: 390 });
  await expect(page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas")).toBeVisible({ timeout: 60_000 });
  if (await handle.getAttribute("aria-pressed") !== "true") {await handle.tap();}
  await expect(handle).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByRole("button", { name: "返回并退出游戏" })).toBeInViewport();
  await page.getByRole("button", { name: "返回并退出游戏" }).tap();
  await page.getByRole("alertdialog", { name: "退出游戏？" }).getByRole("button", { name: "退出游戏", exact: true }).click();
  await expect(page).toHaveURL(/\/$/);
});

test("ACC-MOB-004 phone administration links lead back to play without mounting management controls", async ({ page }) => {
  test.setTimeout(240_000);
  await page.setViewportSize({ width: 390, height: 844 });
  const routes = [
    "/admin/imports", "/admin/imports/new", "/admin/imports/server", "/admin/imports/tasks",
    "/admin/reviews", "/admin/reviews/history", "/admin/games", "/admin/platform-instances",
    "/admin/users", "/admin/bios", "/admin/storage",
  ];
  for (const route of routes) {
    await page.goto(route);
    await expect(page.locator("main").first()).toBeVisible();
    await expectNoDocumentOverflow(page);
    await expectNoTextArrowsInInteractiveControls(page);
    await expect(page.getByRole("heading", { name: "请在电脑上管理游戏库" })).toBeVisible();
    await expect(page.getByRole("navigation", { name: "手机主导航" })).toBeVisible();
    await expect(page.locator("main form, main input, main table")).toHaveCount(0);
  }

  await page.getByRole("link", { name: "返回游戏库", exact: true }).click();
  await expect(page).toHaveURL(/\/library$/);
  await page.setViewportSize({ width: 844, height: 390 });
  await page.goto("/admin/imports");
  await expect(page.getByRole("heading", { name: "请在电脑上管理游戏库" })).toBeVisible();
  await page.setViewportSize({ width: 1280, height: 800 });
  await expect(page.locator(".app-frame")).toBeVisible();
  await expect(page.locator(".phone-admin-notice")).toHaveCount(0);
});

test("ACC-MOB-005 portrait Player validates config before it creates a frame or requests large runtime bytes", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const games = await page.request.get("/api/v1/games?limit=100");
  const game = (await games.json() as { items: Array<{ gameId: string; title: string }> }).items.find((item) => item.title === "Sudoku");
  test.skip(!game, "The launchable acceptance fixture has not been imported.");
  const auth = await login(page);
  const launch = await page.request.post("/api/v1/launches", {
    headers: { Origin: origin, "X-Retrom-Csrf": auth.csrfToken, "Idempotency-Key": crypto.randomUUID() },
    data: {
      gameId: game!.gameId, coreId: null, saveStateId: null, dosEntry: null, returnTo: `/games/${game!.gameId}`,
      clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true },
    },
  });
  expect(launch.status()).toBe(201);
  const playURL = (await launch.json() as { playUrl: string }).playUrl;
  const runtimeRequests: string[] = [];
  page.on("request", (request) => {
    if (!request.url().endsWith("/config")) {runtimeRequests.push(request.url());}
  });
  await page.goto(playURL);
  const gate = page.getByRole("dialog", { name: "请横向握持设备开始游戏" });
  await expect(gate).toBeVisible();
  await expect(page.locator("iframe.player-frame")).toHaveCount(0);
  expect(runtimeRequests.filter((url) => /\/runtime\/launches\/[^/]+\/start|\/runtime\/cores\//.test(url))).toEqual([]);
  await expectNoDocumentOverflow(page);

  for (const viewport of [
    { width: 568, height: 320 },
    { width: 667, height: 375 },
    { width: 844, height: 390 },
    { width: 932, height: 430 },
  ]) {
    await page.setViewportSize(viewport);
    await expect(gate).toHaveCount(0, { timeout: 3_000 });
    await expect(page.locator("iframe.player-frame")).toHaveCount(1);
    await expectNoDocumentOverflow(page);
    const handle = page.getByRole("button", { name: /Player 控制栏/ });
    await expect(handle).toBeVisible();
    const target = await handle.boundingBox();
    expect(target?.width).toBeGreaterThanOrEqual(44);
    expect(target?.height).toBeGreaterThanOrEqual(44);

    if (viewport.width === 568) {
      const player = page.frameLocator("iframe.player-frame");
      await expect(player.locator("canvas.ejs_canvas")).toBeVisible({ timeout: 60_000 });
      const nativeTouchMenu = player.locator(".ejs_virtualGamepad_open");
      await expect(nativeTouchMenu).toHaveCount(1);
      await expect(nativeTouchMenu).toBeHidden();
      const sideControls = player.locator(".ejs_virtualGamepad_left,.ejs_virtualGamepad_right");
      await expect(sideControls).toHaveCount(2);
      for (const control of await sideControls.all()) {await expect(control).toBeVisible();}
      const bottomGaps = await sideControls.evaluateAll((elements) => elements.map((element) => {
        const frameWindow = element.ownerDocument.defaultView!;
        return frameWindow.innerHeight - element.getBoundingClientRect().bottom;
      }));
      expect(bottomGaps).toEqual([70, 70]);
    }

    if (await handle.getAttribute("aria-pressed") === "true") {await handle.click();}
    if (await handle.getAttribute("aria-pressed") !== "true") {await handle.click();}
    await expect(handle).toHaveAttribute("aria-pressed", "true");
    const more = page.getByRole("button", { name: "更多操作" });
    await expect(more).toBeVisible();
    await expect(more).toBeInViewport();
    await more.click();
    const moreMenu = page.getByRole("menu", { name: "Player 更多操作" });
    await expect(moreMenu).toBeVisible();
    const menuBounds = await moreMenu.boundingBox();
    expect(menuBounds?.y).toBeLessThanOrEqual(1);
    expect(menuBounds?.height).toBeGreaterThanOrEqual(viewport.height - 1);
    await moreMenu.getByRole("menuitem", { name: "模拟器设置" }).click();
    const settings = page.getByRole("region", { name: "模拟器设置工具栏" });
    await expect(settings).toBeVisible();
    await settings.getByRole("button", { name: "收起" }).click();
  }
});

test("ACC-MOB-006 landscape Player HUD, sheets and input ownership stay bounded", async ({ page }, testInfo) => {
  test.setTimeout(180_000);
  await page.setViewportSize({ width: 568, height: 320 });
  const games = await page.request.get("/api/v1/games?limit=100");
  const game = (await games.json() as { items: Array<{ gameId: string; title: string }> }).items.find((item) => item.title === "Sudoku");
  test.skip(!game, "The launchable acceptance fixture has not been imported.");
  const auth = await login(page);
  const launch = await page.request.post("/api/v1/launches", {
    headers: { Origin: origin, "X-Retrom-Csrf": auth.csrfToken, "Idempotency-Key": crypto.randomUUID() },
    data: {
      gameId: game!.gameId, coreId: null, saveStateId: null, dosEntry: null, returnTo: `/games/${game!.gameId}`,
      clientCapabilities: { secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true },
    },
  });
  expect(launch.status()).toBe(201);
  await page.goto((await launch.json() as { playUrl: string }).playUrl);
  await expect(page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas")).toBeVisible({ timeout: 60_000 });

  for (const viewport of [
    { width: 568, height: 320 },
    { width: 667, height: 375 },
    { width: 844, height: 390 },
    { width: 932, height: 430 },
  ]) {
    await page.setViewportSize(viewport);
    await expectNoDocumentOverflow(page);
    const handle = page.getByRole("button", { name: /Player 控制栏/ });
    await expect(handle).toBeVisible();
    const handleBounds = await handle.boundingBox();
    expect(handleBounds?.width).toBeGreaterThanOrEqual(44);
    expect(handleBounds?.height).toBeGreaterThanOrEqual(44);
    if (await handle.getAttribute("aria-pressed") !== "true") {await handle.click();}
    await expect(handle).toHaveAttribute("aria-pressed", "true");
    const toolbarHeight = await page.locator(".player-toolbar").evaluate((element) => element.getBoundingClientRect().height);
    expect(toolbarHeight).toBe(48);
    const more = page.getByRole("button", { name: "更多操作" });
    await expect(more).toBeInViewport();
    await more.click();
    const menu = page.getByRole("menu", { name: "Player 更多操作" });
    const menuBounds = await menu.boundingBox();
    expect(menuBounds?.y).toBeLessThanOrEqual(1);
    expect(menuBounds?.height).toBeGreaterThanOrEqual(viewport.height - 1);
    await page.keyboard.press("Escape");
    await expect(menu).toHaveCount(0);
    await page.screenshot({ path: evidencePath(testInfo, `player-${viewport.width}x${viewport.height}.png`) });
  }
});

test("ACC-MOB-007 current mobile, desktop and accessibility baselines remain explicit", async ({ page, context }, testInfo) => {
  test.setTimeout(180_000);
  await page.emulateMedia({ reducedMotion: "reduce" });
  const session = await context.newCDPSession(page);
  await session.send("Emulation.setSafeAreaInsetsOverride", {
    insets: { top: 12, right: 8, bottom: 24, left: 8 },
  });
  try {
    for (const viewport of [
      { width: 320, height: 568 },
      { width: 360, height: 800 },
      { width: 390, height: 844 },
      { width: 412, height: 915 },
      { width: 768, height: 1024 },
      { width: 1024, height: 768 },
    ]) {
      await page.setViewportSize(viewport);
      for (const route of ["/library", "/admin/imports"] as const) {
        await page.goto(route);
        await page.addStyleTag({ content: "html { font-size: 200% !important; }" });
        await expect(page.locator("main").first()).toBeVisible();
        await expectNoDocumentOverflow(page);
        await page.evaluate(axe.source);
        const serious = await page.evaluate(async () => {
          const result = await window.axe.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
          return result.violations.filter((violation) => violation.impact === "serious" || violation.impact === "critical");
        });
        expect(serious).toEqual([]);
        await page.locator("header a").first().focus();
        await page.keyboard.press("Tab");
        await expect(page.locator(":focus").last()).toBeVisible();
      }
      await page.screenshot({
        path: evidencePath(testInfo, `responsive-${viewport.width}x${viewport.height}.png`),
        fullPage: true,
      });
    }
  } finally {
    await session.send("Emulation.setSafeAreaInsetsOverride", { insets: {} });
    await session.detach();
  }
});
