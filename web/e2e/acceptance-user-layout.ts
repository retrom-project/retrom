import { expect, type Page, type TestInfo } from "@playwright/test";
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
      documentHeight: document.documentElement.scrollHeight,
      viewportHeight: document.documentElement.clientHeight,
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
  expect(layout.documentHeight).toBeLessThanOrEqual(layout.viewportHeight);
  await page.screenshot({ path: evidencePath(testInfo, "home-2086x920-css-equivalent.png"), fullPage: true });
}

export async function verifyMobileSavedFeaturedHome(page: Page, testInfo: TestInfo) {
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator(".home-featured-media.has-session-save")).toBeVisible();
  const layout = await page.evaluate(() => {
    const rectangle = (selector: string) => document.querySelector<HTMLElement>(selector)?.getBoundingClientRect() ?? null;
    const media = rectangle(".home-featured-media");
    const cover = rectangle(".home-featured-cover");
    const copy = rectangle(".home-featured-copy");
    const actions = rectangle(".home-featured-actions");
    const preview = rectangle(".home-featured-save-preview");
    if (!media || !cover || !copy || !actions || !preview) {return null;}
    return {
      media: { top: media.top, right: media.right, bottom: media.bottom },
      cover: { top: cover.top, right: cover.right, bottom: cover.bottom },
      copy: { top: copy.top, left: copy.left, right: copy.right, bottom: copy.bottom },
      actions: { right: actions.right, bottom: actions.bottom },
      preview: { top: preview.top, right: preview.right, bottom: preview.bottom },
    };
  });
  expect(layout).not.toBeNull();
  if (!layout) {throw new Error("ACCEPTANCE_MOBILE_SAVED_FEATURED_LAYOUT_UNAVAILABLE");}
  expect(layout.cover.top).toBeGreaterThanOrEqual(layout.media.top);
  expect(layout.copy.top).toBeGreaterThanOrEqual(layout.media.top);
  expect(layout.cover.right).toBeLessThanOrEqual(layout.copy.left);
  expect(layout.copy.right).toBeLessThanOrEqual(layout.media.right + 1);
  expect(layout.actions.right).toBeLessThanOrEqual(layout.media.right + 1);
  expect(layout.actions.bottom).toBeLessThanOrEqual(layout.preview.top - 8);
  expect(layout.preview.right).toBeLessThanOrEqual(layout.media.right + 1);
  expect(layout.preview.bottom).toBeLessThanOrEqual(layout.media.bottom + 1);
  await noPageOverflow(page);
  await page.screenshot({ path: evidencePath(testInfo, "home-mobile-saved-featured.png"), fullPage: true });
}

async function verifyPageLayouts(page: Page) {
  const routes = [
    ["/", ".home-page"], ["/library", ".page-layout-library"], ["/saves", ".page-layout-saves"],
    ["/favorites", ".favorite-page:not(.favorite-loading-shell)"], ["/recent", ".page-layout-recent"], ["/account", ".page-layout-detail"],
    ["/netplay", '.netplay-page:not([role="status"])'],
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
  await expect(page.locator("[data-home-layer]")).toHaveCount(5);
  await expectHomeCoverRatios(page);
  await expect(page.getByText("我的资料库", { exact: true })).toBeVisible();
  if (testInfo.project.name === "chrome-1280") {await page.screenshot({ path: evidencePath(testInfo, "home-layout.png"), fullPage: true });}
  if (testInfo.project.name === "chrome-4k-150") {await verifyPhysical4KHome(page, testInfo);}
}

async function measureHomeLayout(page: Page) {
  return page.evaluate(() => {
    const rect = (selector: string) => document.querySelector<HTMLElement>(selector)?.getBoundingClientRect() ?? null;
    const width = (value: DOMRect | null, fallback: number) => value ? value.width : fallback;
    const height = (value: DOMRect | null) => value ? value.height : 0;
    const ratio = (value: number, total: number) => total ? value / total : 0;
    const topOffset = (parent: DOMRect | null, child: DOMRect | null) => parent && child ? child.top - parent.top : Number.POSITIVE_INFINITY;
    const bottomOffset = (parent: DOMRect | null, child: DOMRect | null) => parent && child ? parent.bottom - child.bottom : Number.POSITIVE_INFINITY;
    const horizontalGap = (left: DOMRect | null, right: DOMRect | null) => left && right ? right.left - left.right : Number.POSITIVE_INFINITY;
    const platform = rect('[data-home-layer="4"]');
    const platformTitle = document.querySelector<HTMLElement>('[data-home-layer="4"] h2')?.getBoundingClientRect() ?? null;
    const homePage = rect(".home-page");
    const appBody = rect(".app-body");
    const featuredMedia = rect(".home-featured-media");
    const featuredCover = rect(".home-featured-cover");
    const featuredCopy = rect(".home-featured-copy");
    const featuredSave = rect(".home-featured-save-preview");
    const featuredActions = rect(".home-featured-actions");
    return {
      fifthLayerBottom: rect('[data-home-layer="5"]')?.bottom ?? Number.POSITIVE_INFINITY,
      viewportHeight: document.documentElement.clientHeight, documentHeight: document.documentElement.scrollHeight,
      homeWidth: width(homePage, 0), appBodyWidth: width(appBody, Number.POSITIVE_INFINITY),
      platformTitleOffset: topOffset(platform, platformTitle),
      featuredCoverHeightRatio: ratio(height(featuredCover), height(featuredMedia)),
      featuredCoverHeight: featuredCover?.height ?? 0, featuredHasSave: Boolean(featuredSave),
      featuredSaveWidthRatio: ratio(width(featuredSave, 0), width(featuredMedia, 0)), featuredSaveWidth: width(featuredSave, 0),
      featuredActionsBottomGap: bottomOffset(featuredMedia, featuredActions),
      featuredCoverActionsBottomDelta: bottomOffset(featuredCover, featuredActions),
      featuredCopyCoverGap: horizontalGap(featuredCover, featuredCopy),
      featuredActionsCoverGap: horizontalGap(featuredCover, featuredActions),
    };
  });
}

async function verifyPhysical4KHome(page: Page, testInfo: TestInfo) {
  const layout = await measureHomeLayout(page);
  expect(await page.evaluate(() => ({ viewport: { width: innerWidth, height: innerHeight }, screen: { width: window.screen.width, height: window.screen.height }, devicePixelRatio: window.devicePixelRatio }))).toEqual({ viewport: { width: 2560, height: 1440 }, screen: { width: 2560, height: 1440 }, devicePixelRatio: 1.5 });
  const screenshot = await page.screenshot({ path: evidencePath(testInfo, "physical-4k-150-home.png") });
  expect(pngDimensions(screenshot)).toEqual({ width: 3840, height: 2160 });
  expect(layout.fifthLayerBottom).toBeLessThanOrEqual(layout.viewportHeight);
  expect(layout.fifthLayerBottom).toBeGreaterThanOrEqual(layout.viewportHeight - 48);
  expect(layout.documentHeight).toBeLessThanOrEqual(layout.viewportHeight);
  expect(layout.homeWidth / layout.appBodyWidth).toBeGreaterThanOrEqual(0.65);
  expect(layout.platformTitleOffset).toBeLessThanOrEqual(20);
  expect(layout.featuredCoverHeightRatio).toBeGreaterThanOrEqual(0.75);
  if (layout.featuredHasSave) {expect(layout.featuredSaveWidthRatio).toBeGreaterThanOrEqual(0.24);}
  expect(layout.featuredActionsBottomGap).toBeLessThanOrEqual(50);
  expect(Math.abs(layout.featuredCoverActionsBottomDelta)).toBeLessThanOrEqual(1);
  expect(layout.featuredCopyCoverGap).toBeGreaterThanOrEqual(23);
  expect(layout.featuredCopyCoverGap).toBeLessThanOrEqual(33);
  expect(Math.abs(layout.featuredActionsCoverGap - layout.featuredCopyCoverGap)).toBeLessThanOrEqual(1);
  await verifyFluidHomeLayout(page, layout.featuredHasSave);
}

async function verifyFluidHomeLayout(page: Page, hasSave: boolean) {
  const measurements: Array<{ coverHeight: number; saveWidth: number; copyGap: number }> = [];
  for (const width of [1900, 2200, 2500, 2800, 3100]) {
    await page.setViewportSize({ width, height: 1250 });
    const layout = await measureHomeLayout(page);
    measurements.push({ coverHeight: layout.featuredCoverHeight, saveWidth: layout.featuredSaveWidth, copyGap: layout.featuredCopyCoverGap });
    expect(Math.abs(layout.featuredCoverActionsBottomDelta)).toBeLessThanOrEqual(1);
    expect(layout.featuredCopyCoverGap).toBeGreaterThanOrEqual(23);
    expect(layout.featuredCopyCoverGap).toBeLessThanOrEqual(33);
  }
  const copyGaps = measurements.map((value) => value.copyGap);
  expect(Math.max(...copyGaps) - Math.min(...copyGaps)).toBeLessThanOrEqual(9);
  for (let index = 1; index < measurements.length; index += 1) {
    expect(measurements[index].coverHeight).toBeGreaterThanOrEqual(measurements[index - 1].coverHeight - 1);
    expect(measurements[index].saveWidth).toBeGreaterThanOrEqual(measurements[index - 1].saveWidth - 1);
  }
  if (hasSave) {expect(measurements.at(-1)?.saveWidth ?? 0).toBeGreaterThan(measurements[0].saveWidth);}
  await page.setViewportSize({ width: 1920, height: 950 });
  const scaled = await measureHomeLayout(page);
  expect(scaled.fifthLayerBottom).toBeLessThanOrEqual(scaled.viewportHeight);
  expect(scaled.documentHeight).toBeLessThanOrEqual(scaled.viewportHeight);
  expect(scaled.platformTitleOffset).toBeLessThanOrEqual(16);
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
  const buffer = await canvas.evaluate((element) => {const value = element as HTMLCanvasElement; return { height: value.height, width: value.width, runtimeAspect: window.EJS_emulator?.gameManager?.getVideoDimensions?.("aspect") ?? 0 };});
  if (!canvasBox) {throw new Error("ACCEPTANCE_PLAYER_CANVAS_UNAVAILABLE");}
  expect(canvasBox.x).toBeGreaterThanOrEqual(-1); expect(canvasBox.y).toBeGreaterThanOrEqual(-1);
  expect(canvasBox.x + canvasBox.width).toBeLessThanOrEqual(dimensions.width + 1); expect(canvasBox.y + canvasBox.height).toBeLessThanOrEqual(dimensions.height + 1);
  expect(Math.abs(canvasBox.width / canvasBox.height - (buffer.runtimeAspect || buffer.width / buffer.height))).toBeLessThanOrEqual(0.01);
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
