import { expect, test, type Page } from "@playwright/test";
import axe from "axe-core";
import { expectHomeCoverRatios, expectNoTextArrowsInInteractiveControls } from "./acceptance-support";

declare global {
  interface Window {
    axe: {
    run: (root: Document, options: object) => Promise<{
      violations: Array<{ id: string; impact: string | null; nodes: unknown[] }>;
    }>;
    };
  }
}

const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000";

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

test.beforeEach(async ({ page }) => { await login(page); });

test("ACC-MOB-001 exact phone and tablet shell baselines have no page overflow", async ({ page }) => {
  const viewports = [
    { width: 320, height: 568 },
    { width: 360, height: 800 },
    { width: 390, height: 844 },
    { width: 412, height: 915 },
    { width: 768, height: 1024 },
    { width: 1024, height: 768 },
  ];
  for (const viewport of viewports) {
    await page.setViewportSize(viewport);
    await page.goto("/library");
    await expect(page.getByRole("heading", { name: "游戏库", exact: true })).toBeVisible();
    await expectNoDocumentOverflow(page);
    if (viewport.width < 768) {
      const bottom = page.getByRole("navigation", { name: "手机主导航" });
      await expect(bottom).toBeVisible();
      await expect(bottom.getByRole("link", { name: "游戏库" })).toHaveAttribute("aria-current", "page");
      await expectMinimumTargets(page, ".mobile-bottom-nav > a, .mobile-bottom-nav > button");
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
  await page.setViewportSize({ width: 390, height: 844 });
  const routes = ["/", "/library", "/saves", "/favorites", "/recent", "/netplay", "/account"];
  for (const route of routes) {
    await page.goto(route);
    await expect(page.locator("main").first()).toBeVisible();
    await expectNoDocumentOverflow(page);
    await expectNoTextArrowsInInteractiveControls(page);
    if (route === "/") {await expectHomeCoverRatios(page);}
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

  const more = page.getByRole("button", { name: "更多导航" });
  await more.click();
  const moreSheet = page.getByRole("dialog", { name: "更多" });
  await expect(moreSheet.getByRole("link", { name: /最近游玩/ })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(more).toBeFocused();

  await page.evaluate(axe.source);
  const serious = await page.evaluate(async () => {
    const result = await window.axe.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    return result.violations.filter((violation) => violation.impact === "serious" || violation.impact === "critical");
  });
  expect(serious).toEqual([]);
});

test("ACC-MOB-004 administrator list and workflow routes use cards or full-width controls", async ({ page }) => {
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
    await expect(page.getByRole("button", { name: "打开主要导航" })).toBeVisible();
    await expect(page.getByRole("navigation", { name: "手机主导航" })).toHaveCount(0);
  }

  await page.goto("/admin/reviews");
  const firstReview = page.locator(".review-workflow-row").first();
  if (await firstReview.count()) {
    await expect(firstReview).toHaveCSS("min-width", "0px");
    await firstReview.getByRole("link", { name: /审核条目|处理条目/ }).click();
    const steps = page.getByRole("navigation", { name: "审核步骤" });
    await expect(steps).toBeVisible();
    await expect(steps.getByRole("link")).toHaveCount(4);
    await expectNoDocumentOverflow(page);
  }
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
  await expect(page.locator('iframe[title="Retrom EmulatorJS Player"]')).toHaveCount(0);
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
    await expect(page.locator('iframe[title="Retrom EmulatorJS Player"]')).toHaveCount(1);
    await expectNoDocumentOverflow(page);
    const handle = page.getByRole("button", { name: /Player 控制栏/ });
    await expect(handle).toBeVisible();
    const target = await handle.boundingBox();
    expect(target?.width).toBeGreaterThanOrEqual(44);
    expect(target?.height).toBeGreaterThanOrEqual(44);

    if (viewport.width === 568) {
      const player = page.frameLocator('iframe[title="Retrom EmulatorJS Player"]');
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
    await handle.click();
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
