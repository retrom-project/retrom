import assert from "node:assert/strict";
import {randomUUID} from "node:crypto";
import {cpSync, mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {basename, join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {createProductClient, directoryFiles, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {focusPreviewCanvas, revealPreviewToolbar, waitForPreviewReady} from "./rpgmaker_preview_actions.mjs";

const origin = process.env.RETROM_ACCEPTANCE_ORIGIN;
assert.ok(origin, "Select the isolated acceptance server");
const directory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/review-screenshot-approval");
mkdirSync(directory, {recursive: true});
const fixture = join(directory, `owned-rpg2000-${randomUUID()}`);
cpSync("testdata/public-roms/rpgmaker-smoke/rpg2000", fixture, {recursive: true});
const ini = join(fixture, "RPG_RT.ini");
writeFileSync(ini, readFileSync(ini, "utf8").replace("FullPackageFlag=1", "FullPackageFlag=0"));
writeFileSync(join(fixture, "acceptance-run.txt"), randomUUID());
const proxy = await localRpgAcceptanceProxy(origin);
const browser = await chromium.launch({executablePath: resolve(".cache/tools/retrom-chrome-for-testing"), headless: true});
const context = await browser.newContext({...proxy.contextOptions, viewport: {width: 1280, height: 900}});
try {
  const login = await context.request.post(`${origin}/api/v1/auth/login`, {
    headers: {Origin: origin}, data: {username: "test", password: "test"},
  });
  assert.equal(login.status(), 200);
  const client = createProductClient(context, origin, (await login.json()).csrfToken);
  const directories = await client.json("GET", "/api/v1/admin/platform-instances?platformId=rpgmaker&limit=100");
  const platform = directories.items.find(value => value.defaultCoreId === "rpgmaker");
  assert.ok(platform);
  const uploadId = await client.upload(directoryFiles(fixture, `${basename(fixture)}/`), "DIRECTORY");
  const imported = await client.json("POST", "/api/v1/admin/imports", {
    headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: platform.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []},
  });
  let before = await reviewForImport(client, imported.importJobId);
  assert.equal(before.metadata.title, basename(fixture));
  await client.json("PATCH", `/api/v1/admin/reviews/${before.itemId}`, {
    headers: {...client.writeHeaders(), "If-Match": `"v${before.version}"`},
    data: {metadata: {...before.metadata, title: "RTP Screenshot Acceptance"}, tagIds: []},
  });
  before = await client.json("GET", `/api/v1/admin/reviews/${before.itemId}`);
  console.log("Imported owned project with RTP blocker");
  assert.equal(before.readiness.compatibilityCode, "RPG_EXTERNAL_RTP_REQUIRED");
  assert.equal(before.canApprove, false);
  const review = await context.newPage();
  await review.goto(`${origin}/admin/reviews/${before.itemId}`);
  await review.getByRole("button", {name: "通过并发布", exact: true}).waitFor();
  assert.equal(await review.getByRole("button", {name: "通过并发布", exact: true}).isDisabled(), true);
  await review.screenshot({path: join(directory, "before-screenshot.png"), fullPage: true});
  const preview = await client.json("POST", `/api/v1/admin/reviews/${before.itemId}/previews`, {
    headers: {...client.writeHeaders(), "If-Match": `"v${before.version}"`}, expected: 201,
    data: {clientCapabilities: {secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true}},
  });
  const player = await context.newPage();
  await player.goto(new URL(preview.playUrl, origin).href);
  await waitForPreviewReady(player);
  console.log("Real preview ready");
  const canvas = await focusPreviewCanvas(player);
  console.log("Canvas visible", await canvas.evaluate(node => ({width: node.width, height: node.height})));
  await player.waitForTimeout(1000);
  await canvas.screenshot({path: join(directory, "real-game-canvas.png")});
  await revealPreviewToolbar(player);
  await player.screenshot({path: join(directory, "runtime-before-screenshot.png")});
  try {
    const [response] = await Promise.all([
      player.waitForResponse(response => response.request().method() === "POST" &&
        new URL(response.url()).pathname.endsWith("/review-screenshot"), {timeout: 15_000}),
      player.getByRole("button", {name: "保存审核截图", exact: true}).click(),
    ]);
    assert.equal(response.status(), 201);
  } catch (error) {
    console.error((await player.locator("body").innerText()).slice(0, 1800));
    await player.screenshot({path: join(directory, "runtime-screenshot-failed.png")});
    throw error;
  }
  const after = await client.json("GET", `/api/v1/admin/reviews/${before.itemId}`);
  assert.equal(after.canApprove, true);
  assert.equal(after.rpgMaker.selfContainedOverride, false);
  assert.equal(after.readiness.status, before.readiness.status);
  assert.equal(after.readiness.compatibilityCode, before.readiness.compatibilityCode);
  assert.deepEqual(after.readiness.dependencySnapshot, before.readiness.dependencySnapshot);
  await review.bringToFront();
  await review.reload();
  await review.getByText("可凭截图发布", {exact: true}).waitFor();
  assert.equal(await review.getByRole("button", {name: "通过并发布", exact: true}).isEnabled(), true);
  assert.ok(await review.locator(".review-validation-guidance").getByText("RPG_EXTERNAL_RTP_REQUIRED", {exact: true}).isVisible());
  await review.screenshot({path: join(directory, "after-screenshot.png"), fullPage: true});
  for (const [name, width, height, deviceScaleFactor] of [["mobile", 390, 844, 1], ["4k-150", 2560, 1440, 1.5]]) {
    const view = await browser.newContext({...proxy.contextOptions, storageState: await context.storageState(), viewport: {width, height}, deviceScaleFactor});
    try {
      const page = await view.newPage();
      await page.goto(`${origin}/admin/reviews/${before.itemId}`);
      if (width === 390) {await page.getByText("请在电脑上管理游戏库", {exact: true}).waitFor();}
      else {await page.getByText("可凭截图发布", {exact: true}).waitFor();}
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
      await page.screenshot({path: join(directory, `${name}-approval.png`)});
    } finally {await view.close();}
  }
  const approved = await client.json("POST", `/api/v1/admin/reviews/${before.itemId}/approve`, {
    headers: {...client.writeHeaders(), "If-Match": `"v${after.version}"`}, data: {reason: null}, expected: 201,
  });
  assert.ok(approved.gameId);
  writeFileSync(join(directory, "result.json"), JSON.stringify({status: "PASS", itemId: before.itemId,
    gameId: approved.gameId, blocker: after.readiness.compatibilityCode,
    evidence: "owned RPG2000 fixture, FullPackageFlag=0, actual runtime screenshot and approval"}, null, 2));
  console.log("review screenshot approval: PASS (real runtime, preserved RTP blockers, three viewports)");
} finally {
  await context.close();
  await browser.close();
  await proxy.close();
}
