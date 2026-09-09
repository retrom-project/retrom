import assert from "node:assert/strict";
import {mkdirSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, launchCart, gamepad, saveCart} from "./fantasy_product_client.mjs";
import {px68kLocalProxy, importPX68KDisk, px68kCanvas, canvasDigest} from "./px68k_product_support.mjs";
const baseUrl = process.env.RETROM_ACCEPTANCE_BASE_URL;
const gameId = process.env.RETROM_PX68K_GAME_ID, saveStateId = process.env.RETROM_PX68K_SAVE_STATE_ID;
const directory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/px68k-input-product");
mkdirSync(directory, {recursive: true});
const evidence = {caseId: "ACC-PX68K-002", status: "FAIL", stages: [], errors: []};
let browser, proxy;
try {
  assert.ok(baseUrl && gameId && saveStateId && process.env.RETROM_CHROME_EXECUTABLE, "PX68K_INPUT_ACCEPTANCE_INPUT_REQUIRED");
  const {chromium} = await import(process.env.RETROM_PLAYWRIGHT_MODULE ?? "../../web/node_modules/playwright/index.mjs");
  proxy = await px68kLocalProxy(baseUrl);
  evidence.browserMode = process.env.RETROM_ACCEPTANCE_HEADED === "1" ? "HEADED" : "HEADLESS";
  browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE,
    headless: evidence.browserMode === "HEADLESS", args: ["--use-angle=swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1440, height: 1000}, ...proxy.contextOptions});
  context.setDefaultTimeout(15000); await installVirtualStandardGamepad(context);
  const client = await fantasyClient(context, baseUrl);
  const reviewId = process.env.RETROM_PX68K_REVIEW_ID ?? (await importPX68KDisk(client, process.env.RETROM_PX68K_PREVIEW_DISK)).itemId;
  evidence.reviewId = reviewId; console.log("PX68K input: review ready");
  const preview = await open(context, await previewCart(client, reviewId));
  console.log("PX68K input: preview mounted");
  try {await waitPreview(preview);}
  catch (error) {await preview.canvas.screenshot({path: join(directory, "preview-failed.png")}); throw error;}
  await snapshot(preview, "preview", 1); await preview.page.close();
  evidence.stages.push("review-preview");
  const launch = await launchCart(client, gameId, saveStateId), opened = await open(context, launch);
  await opened.page.waitForTimeout(800); await snapshot(opened, "before-input");
  await gamepad(opened.page, 1, 120); await opened.page.waitForTimeout(500); await snapshot(opened, "button-1");
  await opened.canvas.press("Escape", {delay: 120}); await snapshot(opened, "keyboard-pause");
  await opened.canvas.press("Escape", {delay: 120}); await snapshot(opened, "keyboard-resume");
  await opened.canvas.press("ArrowLeft", {delay: 400}); await opened.canvas.press("z", {delay: 400});
  await snapshot(opened, "keyboard-left-fire");
  const saved = await saveCart(opened.page, launch.launchId, "px68k");
  evidence.launchId = launch.launchId; evidence.saveStateId = saved.saveStateId;
  await opened.page.close(); evidence.stages.push("product-button-1-keyboard-save");
  const restored = await launchCart(client, gameId, saved.saveStateId), resumed = await open(context, restored);
  assert.notEqual(restored.launchId, launch.launchId);
  await resumed.page.waitForTimeout(800); await snapshot(resumed, "restored");
  await gamepad(resumed.page, 1, 120); await resumed.canvas.press("ArrowRight", {delay: 400});
  await resumed.canvas.press("z", {delay: 400}); await snapshot(resumed, "restored-input");
  evidence.restoredLaunchId = restored.launchId;
  evidence.restoredSaveStateId = (await saveCart(resumed.page, restored.launchId, "px68k")).saveStateId;
  await resumed.page.close(); evidence.stages.push("fresh-launch-restore-input-save");
  assert.deepEqual(evidence.errors, []);
  // Palette cycling can change pixels even while the guest is paused; screenshots
  // must be reviewed for PAUSE, ship movement and shooting before declaring PASS.
  evidence.status = "AWAITING_VISUAL_REVIEW";
} catch (error) {
  evidence.errorCode = /^[A-Z][A-Z0-9_]+$/u.test(error.message) ? error.message : "PX68K_INPUT_ACCEPTANCE_FAILED";
  process.exitCode = 1;
} finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "px68k-input-product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}
async function open(context, launch) {
  const page = await context.newPage();
  page.on("pageerror", error => evidence.errors.push(error.message.slice(0, 200)));
  page.on("response", async response => {
    if (!launch.launchId || !response.url().endsWith(`/runtime/launches/${launch.launchId}/config`) || response.status() !== 200) {return;}
    const envelope = await response.json();
    evidence.runtime = {providerId: envelope.runtime.providerId, targetId: envelope.runtime.targetId,
      bundleSha256: envelope.runtime.bundleSha256, providerVersion: envelope.runtime.providerVersion};
  });
  await page.goto(`${baseUrl}${launch.playUrl}`, {waitUntil: "domcontentloaded", timeout: 60000});
  return {page, canvas: await px68kCanvas(page)};
}
async function snapshot(opened, name, minimumColors = 4) {
  const frame = await canvasDigest(opened.canvas);
  assert.ok(frame.colors > minimumColors, "PX68K_INPUT_FRAME_EMPTY");
  await opened.canvas.screenshot({path: join(directory, `${name}.png`)});
  evidence[name] = frame;
}

async function waitPreview(opened) {
  for (let attempt = 0; attempt < 60; attempt++) {
    if ((await canvasDigest(opened.canvas)).colors > 1) {return;}
    await opened.page.waitForTimeout(1000);
  }
  throw Error("PX68K_PREVIEW_BOOT_SCREEN_TIMEOUT");
}
