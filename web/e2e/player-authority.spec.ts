import {readFileSync} from "node:fs";
import path from "node:path";
import {expect, test, type Page} from "@playwright/test";
import {evidencePath} from "./acceptance-support";
import {createOrdinaryImport, login} from "./issue-regression-support";
import {runtimeFrameCount} from "./runtime-provider-support";

test("ACC-GAME-003 deletion stops running and paused Players after authoritative renewal", async ({page, browser}, testInfo) => {
  test.skip(testInfo.project.name !== "chrome-1280");
  test.setTimeout(180_000);
  const headers = await login(page);
  const gameId = await publishFixture(page, headers);
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const standardContext = await browser.newContext({baseURL: origin});
  const immersiveContext = await browser.newContext({baseURL: origin, viewport: {width: 2560, height: 1440}, deviceScaleFactor: 1.5});
  try {
    const standard = await standardContext.newPage(), immersive = await immersiveContext.newPage();
    await launchFixture(standard, gameId, false);
    await launchFixture(immersive, gameId, true);
    await immersive.bringToFront();
    await immersive.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas").click();
    await immersive.keyboard.press("m");
    await expect(immersive.getByRole("dialog", {name: "游戏菜单"})).toBeVisible();
    await immersive.screenshot({path: evidencePath(testInfo, "paused-menu-4k-150.png")});
    // A transient 503 cannot revoke a healthy game.
    await standard.route("**/runtime/launches/*/renew", route => route.fulfill({status: 503}));
    await standard.bringToFront();
    const before = await runtimeFrameCount(standard);
    await standard.waitForTimeout(16_000);
    expect(await runtimeFrameCount(standard)).toBeGreaterThan(before);
    await expect(standard.locator(".player-loading")).toHaveCount(0);
    await standard.unroute("**/runtime/launches/*/renew");
    await standardContext.setOffline(true);
    const detail = await page.request.get(`/api/v1/admin/games/${gameId}`);
    const game = await detail.json() as {title: string; deleteImpact: {impactDigest: string}};
    const deleted = await page.request.delete(`/api/v1/admin/games/${gameId}`, {
      headers: {...headers, "If-Match": detail.headers().etag, "Idempotency-Key": crypto.randomUUID()},
      data: {confirmTitle: game.title, impactDigest: game.deleteImpact.impactDigest},
    });
    expect(deleted.status(), await deleted.text()).toBe(202);
    await immersive.bringToFront();
    await expect(immersive.locator(".player-loading[role=alert]")).toContainText("运行会话已不可用", {timeout: 22_000});
    await expect(immersive.locator("iframe.player-frame")).toHaveCount(0);
    await expect(immersive.getByRole("dialog", {name: "游戏菜单"})).toHaveCount(0);
    await immersive.screenshot({path: evidencePath(testInfo, "revoked-immersive-4k-150.png")});
    await standard.bringToFront();
    const offlineFrame = await runtimeFrameCount(standard);
    await expect.poll(() => runtimeFrameCount(standard)).toBeGreaterThan(offlineFrame + 10);
    await expect(standard.locator(".player-loading")).toHaveCount(0);
    await standardContext.setOffline(false);
    await expect(standard.locator(".player-loading[role=alert]")).toContainText("运行会话已不可用", {timeout: 22_000});
    await expect(standard.locator("iframe.player-frame")).toHaveCount(0);
    await standard.screenshot({path: evidencePath(testInfo, "revoked-standard-desktop.png")});
    await standard.setViewportSize({width: 390, height: 844});
    await standard.screenshot({path: evidencePath(testInfo, "revoked-standard-mobile.png")});
    await standard.getByRole("link", {name: "返回游戏库"}).click();
    await expect(standard).toHaveURL(/\/library$/);
  } finally {
    await standardContext.close();
    await immersiveContext.close();
  }
});

async function publishFixture(page: Page, headers: Record<string, string>) {
  // Give the owned fixture a fresh header identity, preserving its program and GBA checksum.
  const bytes = readFileSync(path.join(process.cwd(), "../testdata/public-roms/gba-smoke/gba-smoke.gba"));
  bytes.write(crypto.randomUUID().replaceAll("-", "").slice(0, 12), 0xa0, "ascii");
  bytes[0xbd] = (-bytes.subarray(0xa0, 0xbd).reduce((sum, byte) => sum + byte, 0) - 0x19) & 0xff;
  const imported = await createOrdinaryImport(page, headers, bytes);
  let itemId = "";
  await expect.poll(async () => {
    const response = await page.request.get(`/api/v1/admin/reviews?importJobId=${imported.importJobId}`);
    const reviews = await response.json() as {items: {itemId: string}[]};
    itemId = reviews.items[0]?.itemId ?? "";
    return itemId;
  }, {timeout: 30_000}).not.toBe("");
  const detail = await page.request.get(`/api/v1/admin/reviews/${itemId}`);
  const response = await page.request.post(`/api/v1/admin/reviews/${itemId}/approve`, {
    headers: {...headers, "If-Match": detail.headers().etag, "Idempotency-Key": crypto.randomUUID()}, data: {reason: null},
  });
  expect(response.ok(), await response.text()).toBe(true);
  return (await response.json() as {gameId: string}).gameId;
}

async function launchFixture(page: Page, gameId: string, immersive: boolean) {
  const headers = await login(page);
  const response = await page.request.post("/api/v1/launches", {
    headers: {...headers, "Idempotency-Key": crypto.randomUUID()},
    data: {gameId, coreId: "mgba", saveStateId: null, dosEntry: null, returnTo: immersive ? `/immersive/platforms/gba?gameId=${gameId}` : "/library",
      clientCapabilities: {secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true}},
  });
  expect(response.status(), await response.text()).toBe(201);
  const launch = await response.json() as {playUrl: string};
  const url = new URL(launch.playUrl, process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000");
  if (immersive) {url.searchParams.set("experience", "immersive");}
  await page.goto(url.href);
  await expect(page.locator(".player-loading")).toBeHidden({timeout: 60_000});
  await expect.poll(() => runtimeFrameCount(page)).toBeGreaterThan(30);
}
