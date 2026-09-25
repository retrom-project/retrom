import { expect, type Page, type TestInfo } from "@playwright/test";
import { expectNaturalHomeFlow } from "./home-layout-support";
import { evidencePath, expectHomeCoverRatios, expectNoTextArrowsInInteractiveControls, noPageOverflow, pageCanvasGaps, pngDimensions, type HorizontalGaps } from "./acceptance-support";

export async function verifyUserDesktopLayouts(page: Page, testInfo: TestInfo) {
  await page.addInitScript(() => {Object.defineProperty(document.documentElement, "requestFullscreen", { configurable: true, value: () => Promise.resolve() });});
  const sharedGaps = await verifyPageLayouts(page);
  await verifyHomeLayout(page, testInfo);
  await verifyLibraryAndPlayerLayout(page, testInfo, sharedGaps);
}

export async function verifyCompactFeaturedHome(page: Page, testInfo: TestInfo) {
  await page.setViewportSize({ width: 2086, height: 920 });
  await expectHomeCoverRatios(page);
  await expectNoTextArrowsInInteractiveControls(page);
  await expect(page.locator(".home-featured-details")).toBeVisible();
  await expect(page.locator(".home-featured-actions")).toBeVisible();
  const layout = await page.evaluate(() => {
    const rectangle = (selector: string) => document.querySelector<HTMLElement>(selector)?.getBoundingClientRect() ?? null;
    const media = rectangle(".home-featured-media");
    const cover = rectangle(".home-featured-cover");
    const copy = rectangle(".home-featured-copy");
    const actions = rectangle(".home-featured-actions");
    if (!media || !cover || !copy || !actions) {return null;}
    return {
      media: { top: media.top, bottom: media.bottom, height: media.height },
      cover: { top: cover.top, bottom: cover.bottom },
      copy: { top: copy.top, bottom: copy.bottom },
      actionsBottom: actions.bottom,
    };
  });
  expect(layout).not.toBeNull();
  if (!layout) {throw new Error("ACCEPTANCE_COMPACT_FEATURED_LAYOUT_UNAVAILABLE");}
  expect(layout.media.height).toBeGreaterThanOrEqual(160);
  expect(layout.cover.top).toBeGreaterThanOrEqual(layout.media.top);
  expect(layout.cover.bottom).toBeLessThanOrEqual(layout.media.bottom + 1);
  expect(layout.copy.top).toBeGreaterThanOrEqual(layout.media.top - 1);
  expect(layout.copy.bottom).toBeLessThanOrEqual(layout.media.bottom + 1);
  expect(layout.actionsBottom).toBeLessThanOrEqual(layout.media.bottom + 1);
  await expectNaturalHomeFlow(page);
  await page.screenshot({ path: evidencePath(testInfo, "home-2086x920-css-equivalent.png"), fullPage: true });
}

export async function verifyMobileSavedFeaturedHome(page: Page, testInfo: TestInfo) {
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator(".phone-continue-card")).toBeVisible();
  const layout = await page.evaluate(() => {
    const rectangle = (selector: string) => document.querySelector<HTMLElement>(selector)?.getBoundingClientRect();
    const card = rectangle(".phone-continue-card");
    const cover = rectangle(".phone-continue-card .phone-game-poster");
    const copy = rectangle(".phone-continue-copy");
    const action = rectangle(".phone-continue-copy .button");
    if (!card || !cover || !copy || !action) {return null;}
    return { cardRight: card.right, coverRight: cover.right, copyLeft: copy.left,
      actionRight: action.right, actionHeight: action.height, coverRatio: cover.width / cover.height };
  });
  expect(layout).not.toBeNull();
  if (!layout) {throw new Error("ACCEPTANCE_MOBILE_SAVED_FEATURED_LAYOUT_UNAVAILABLE");}
  expect(layout.coverRight).toBeLessThanOrEqual(layout.copyLeft);
  expect(layout.actionRight).toBeLessThanOrEqual(layout.cardRight + 1);
  expect(layout.actionHeight).toBeGreaterThanOrEqual(44);
  expect(layout.coverRatio).toBeCloseTo(5 / 7, 2);
  await expect(page.getByRole("button", { name: "从存档继续", exact: true })).toBeVisible();
  await noPageOverflow(page);
  await page.screenshot({ path: evidencePath(testInfo, "home-mobile-saved-featured.png"), fullPage: true });
}

async function verifyPageLayouts(page: Page) {
  const routes = [
    ["/", ".home-page"], ["/library", ".page-layout-library"], ["/saves", ".page-layout-saves"],
    ["/favorites", ".favorite-page:not(.favorite-loading-shell)"], ["/recent", ".page-layout-recent"], ["/account", ".page-layout-detail"],
  ] as const;
  let shared: HorizontalGaps | null = null;
  for (const [route, selector] of routes) {
    await page.goto(route);
    const layout = page.locator(selector);
    await expect(layout).toBeVisible();
    await expect(layout.locator(".page-header")).toBeVisible();
    await expectNoTextArrowsInInteractiveControls(page);
    await noPageOverflow(page);
    const gaps = await pageCanvasGaps(page, selector);
    if (shared) {
      expect(Math.abs(gaps.left - shared.left)).toBeLessThanOrEqual(1);
      expect(Math.abs(gaps.right - shared.right)).toBeLessThanOrEqual(1);
    } else {shared = gaps;}
  }
  if (!shared) {throw new Error("ACCEPTANCE_SHARED_LAYOUT_UNAVAILABLE");}
  return shared;
}

async function verifyHomeLayout(page: Page, testInfo: TestInfo) {
  await page.goto("/");
  await expect(page.locator("[data-home-layer]")).toHaveCount(4);
  await expectHomeCoverRatios(page);
  await expect(page.getByText("我的资料库", { exact: true })).toBeVisible();
  if (testInfo.project.name === "chrome-1280") {await page.screenshot({ path: evidencePath(testInfo, "home-layout.png"), fullPage: true });}
  if (testInfo.project.name === "chrome-4k-150") {await verifyPhysical4KHome(page, testInfo);}
}

async function verifyPhysical4KHome(page: Page, testInfo: TestInfo) {
  expect(await page.evaluate(() => ({ viewport: { width: innerWidth, height: innerHeight }, screen: { width: window.screen.width, height: window.screen.height }, devicePixelRatio: window.devicePixelRatio }))).toEqual({ viewport: { width: 2560, height: 1440 }, screen: { width: 2560, height: 1440 }, devicePixelRatio: 1.5 });
  const screenshot = await page.screenshot({ path: evidencePath(testInfo, "physical-4k-150-home.png") });
  expect(pngDimensions(screenshot)).toEqual({ width: 3840, height: 2160 });
  await expectNaturalHomeFlow(page);
  for (const width of [1900, 2200, 2500, 2800, 3100]) {
    await page.setViewportSize({ width, height: 1250 });
    await expectHomeCoverRatios(page);
    await noPageOverflow(page);
  }
  await page.setViewportSize({ width: 2560, height: 1440 });
}

async function verifyLibraryAndPlayerLayout(page: Page, testInfo: TestInfo, sharedGaps: HorizontalGaps) {
  await page.goto("/library");
  await expect(page.getByRole("heading", { name: "游戏库" })).toBeVisible();
  const game = page.locator(".library-game-card").filter({ hasText: "Sudoku" });
  await expect(game).toBeVisible();
  const card = await game.evaluate((element) => {const box = element.getBoundingClientRect(); const cover = element.querySelector(".library-game-cover")?.getBoundingClientRect(); return { width: box.width, ratio: cover ? cover.width / cover.height : 0 };});
  expect(card.width).toBeGreaterThanOrEqual(269); expect(card.width).toBeLessThanOrEqual(321); expect(Math.abs(card.ratio - 0.75)).toBeLessThanOrEqual(0.01);
  await game.getByRole("link").first().click();
  await expect(page.getByRole("button", { name: "开始游戏" })).toBeVisible();
  await noPageOverflow(page);
  const detailGaps = await pageCanvasGaps(page, ".game-detail-page");
  expect(Math.abs(detailGaps.left - sharedGaps.left)).toBeLessThanOrEqual(1); expect(Math.abs(detailGaps.right - sharedGaps.right)).toBeLessThanOrEqual(1);
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/);
  await verifyPlayerCanvas(page);
  await page.screenshot({ path: evidencePath(testInfo, "user-layout.png"), fullPage: true });
}

async function verifyPlayerCanvas(page: Page) {
  const shell = page.locator(".player-shell"); const stage = page.locator(".player-stage");
  await expect(shell).toBeVisible();
  const dimensions = await page.evaluate(() => ({ height: innerHeight, width: innerWidth }));
  expect(await shell.boundingBox()).toEqual({ x: 0, y: 0, width: dimensions.width, height: dimensions.height });
  expect(await stage.boundingBox()).toEqual({ x: 0, y: 0, width: dimensions.width, height: dimensions.height });
  await expect(page.locator(".player-loading")).toBeHidden({ timeout: 30_000 });
  const toolbar = page.locator(".player-toolbar");
  await expect(toolbar).toHaveCSS("opacity", "0", { timeout: 5_000 });
  const canvas = page.frameLocator(".player-frame").locator("canvas").first();
  await expect(canvas).toBeVisible();
  const canvasBox = await canvas.boundingBox();
  const buffer = await canvas.evaluate((element) => {const value = element as HTMLCanvasElement; return { height: value.height, width: value.width };});
  if (!canvasBox) {throw new Error("ACCEPTANCE_PLAYER_CANVAS_UNAVAILABLE");}
  expect(canvasBox.x).toBeGreaterThanOrEqual(-1); expect(canvasBox.y).toBeGreaterThanOrEqual(-1);
  expect(canvasBox.x + canvasBox.width).toBeLessThanOrEqual(dimensions.width + 1); expect(canvasBox.y + canvasBox.height).toBeLessThanOrEqual(dimensions.height + 1);
  expect(Math.abs(canvasBox.width / canvasBox.height - buffer.width / buffer.height)).toBeLessThanOrEqual(0.01);
  expect(Math.min(Math.abs(canvasBox.width - dimensions.width), Math.abs(canvasBox.height - dimensions.height))).toBeLessThanOrEqual(2);
  expect(Math.abs(canvasBox.x - (dimensions.width - canvasBox.width) / 2)).toBeLessThanOrEqual(2); expect(Math.abs(canvasBox.y - (dimensions.height - canvasBox.height) / 2)).toBeLessThanOrEqual(2);
  await page.mouse.move(dimensions.width / 2, dimensions.height / 2); await expect(toolbar).toHaveCSS("opacity", "0");
  await canvas.click({ position: { x: Math.max(1, canvasBox.width / 2), y: Math.max(1, canvasBox.height / 2) } });
  for (const key of ["w", "j", "ArrowUp", "5"]) {
    await page.keyboard.press(key);
    await expect(toolbar).toHaveCSS("opacity", "0");
  }
  await page.mouse.move(dimensions.width / 2, 1); await expect(toolbar).toHaveCSS("opacity", "1");
  await page.mouse.move(dimensions.width / 2, dimensions.height / 2); await expect(toolbar).toHaveCSS("opacity", "0");
  await noPageOverflow(page);
}
