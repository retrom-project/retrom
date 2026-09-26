import { expect, type Page, type TestInfo } from "@playwright/test";
import { evidencePath } from "./acceptance-support";
import { expectNaturalHomeFlow } from "./home-layout-support";
import {seedHomeState, uiLayoutState} from "./ui-layout-state";

export async function expectHomeStates(page: Page, testInfo: TestInfo) {
  const database = process.env.RETROM_E2E_DATABASE;
  if (!database) { return; } // Live PFB review remains read-only.
  uiLayoutState("isolate");
  try {
    await verifyHomeStates(page, testInfo);
  } finally {uiLayoutState("restore");}
}

async function verifyHomeStates(page: Page, testInfo: TestInfo) {
  for (const state of ["empty", "played", "saved"] as const) {
    seedHomeState(state);
    await page.goto("/");
    await expect(page.locator(".home-featured-empty")).toHaveCount(state === "empty" ? 1 : 0);
    if (state !== "empty") {
      // The published fixture cover and the UI-only checkpoint picture are portrait.
      await expect(page.locator(".home-featured-media")).toHaveAttribute("data-kind", "platform");
      await expect(page.locator(".home-featured-art.is-ready")).toBeVisible();
      await expect(page.locator(".home-featured-art")).toHaveAttribute("src", /\/images\/platforms\//);
    }
    await expect(page.locator(".home-scene-caption")).toHaveCount(0);
    await expectNaturalHomeFlow(page);
    await page.screenshot({ path: evidencePath(testInfo, `ui-home-${state}.png`), fullPage: true });
  }
  await expectLandscapeAndEmptyMedia(page, testInfo);
}

async function expectLandscapeAndEmptyMedia(page: Page, testInfo: TestInfo) {
  const data = await (await page.request.get("/api/v1/home")).json();
  const screenshot = new URL(data.featuredGame.lastSessionSave.screenshotUrl, page.url()).href;
  // Layout-only response verifies orientation and failure handling; it is never launched.
  await page.route(screenshot, (route) => route.fulfill({ contentType: "image/svg+xml", body: '<svg xmlns="http://www.w3.org/2000/svg" width="320" height="180"><rect width="320" height="180" fill="#314466"/></svg>' }));
  await page.reload();
  await expect(page.locator(".home-scene-caption")).toBeVisible();
  const before = await heroGeometry(page);
  await page.screenshot({ path: evidencePath(testInfo, "ui-home-landscape.png"), fullPage: true });
  await page.unroute(screenshot);
  await page.route(screenshot, (route) => route.abort());
  await page.route("**/images/platforms/**", (route) => route.abort());
  await page.reload();
  await expect(page.locator(".home-featured-media")).toHaveAttribute("data-kind", "empty");
  await expect(page.locator(".home-platform-card img")).toHaveCount(0);
  await expect(page.locator(".home-platform-card .home-platform-art")).toBeVisible();
  expect(await heroGeometry(page)).toEqual(before);
  await page.screenshot({ path: evidencePath(testInfo, "ui-home-no-media.png"), fullPage: true });
  await page.unroute(screenshot);
  await page.unroute("**/images/platforms/**");
}

async function heroGeometry(page: Page) {
  return page.locator(".home-featured-panel, .home-featured-copy, .home-featured-actions, .home-platform-card > a > span, .home-platform-art").evaluateAll((elements) => elements.map((element) => {
    const { x, y, width, height } = element.getBoundingClientRect();
    return { x, y, width, height };
  }));
}
