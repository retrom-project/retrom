import assert from "node:assert/strict";
import {mkdirSync, writeFileSync, readFileSync, existsSync} from "node:fs";
import {createHash} from "node:crypto";
import {join, resolve} from "node:path";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {observeFantasyAudio, fantasyAudioEvidence} from "./fantasy_fixture.mjs";
import {fantasyClient, previewCart, approveCart, launchCart, gamepad, saveCart} from "./fantasy_product_client.mjs";
import {px68kLocalProxy, installPX68KBIOS, importPX68KDisk, px68kCanvas, canvasDigest, waitPX68KGame} from "./px68k_product_support.mjs";
const baseUrl = process.env.RETROM_ACCEPTANCE_BASE_URL, game = process.env.RETROM_PX68K_DISK;
const directory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/px68k-product");
mkdirSync(directory, {recursive: true});
const evidence = {browserMode: process.env.RETROM_ACCEPTANCE_HEADED === "1" ? "HEADED" : "HEADLESS", renderer: "swiftshader", schemaVersion: 1, caseId: "ACC-PX68K-001", status: "FAIL", errors: [], stages: []};
let browser, proxy;
try {
  assert.ok(baseUrl && game && process.env.RETROM_PX68K_BIOS_DIR && process.env.RETROM_CHROME_EXECUTABLE,
    "PX68K_ACCEPTANCE_INPUT_REQUIRED");
  const {chromium} = await import(process.env.RETROM_PLAYWRIGHT_MODULE ?? "../../web/node_modules/playwright/index.mjs");
  proxy = await px68kLocalProxy(baseUrl);
  browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: process.env.RETROM_ACCEPTANCE_HEADED !== "1", args: ["--use-angle=swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1440, height: 1000}, ...proxy.contextOptions});
  context.setDefaultTimeout(15000);
  await installVirtualStandardGamepad(context); await observeFantasyAudio(context);
  const client = await fantasyClient(context, baseUrl);
  await installPX68KBIOS(client, process.env.RETROM_PX68K_BIOS_DIR);
  const digest = createHash("sha256").update(readFileSync(game)).digest("hex");
  const progressPath = join(directory, "product-input.json");
  const progress = existsSync(progressPath) ? JSON.parse(readFileSync(progressPath)) : {};
  if (progress.digest) {assert.equal(progress.digest, digest, "PX68K_ACCEPTANCE_INPUT_CHANGED");}
  const review = progress.review ?? await importPX68KDisk(client, game);
  writeFileSync(progressPath, JSON.stringify({digest, review, gameId: progress.gameId}));
  const preview = await previewCart(client, review.itemId);
  console.log("PX68K: opening review preview");
  const previewPage = await open(context, preview);
  console.log("PX68K: canvas mounted");
  await waitPX68KGame(previewPage.canvas);
  await previewPage.canvas.screenshot({path: join(directory, "preview.png")});
  evidence.preview = await canvasDigest(previewPage.canvas);
  assert.ok(evidence.preview.colors > 4, "PX68K_PREVIEW_EMPTY");
  await previewPage.page.close(); evidence.stages.push("import-review-preview");
  const gameId = progress.gameId ?? (await approveCart(client, review.itemId)).gameId;
  writeFileSync(progressPath, JSON.stringify({digest, review, gameId}));
  evidence.gameId = gameId; evidence.gameSha256 = digest;
  const launch = await launchCart(client, gameId), opened = await open(context, launch);
  await waitPX68KGame(opened.canvas);
  await gamepad(opened.page, 9); await gamepad(opened.page, 0, 500);
  await opened.page.waitForTimeout(2000);
  const before = await canvasDigest(opened.canvas);
  await gamepad(opened.page, 15, 800); await gamepad(opened.page, 0, 800);
  evidence.afterInput = await canvasDigest(opened.canvas);
  assert.notEqual(evidence.afterInput.sha256, before.sha256, "PX68K_FRAMES_NOT_ADVANCING");
  await opened.canvas.screenshot({path: join(directory, "gameplay.png")});
  evidence.audio = await fantasyAudioEvidence(opened.page);
  assert.ok(evidence.audio.nonzeroBuffers > 0, "PX68K_AUDIO_MISSING");
  const saved = await saveCart(opened.page, launch.launchId, "px68k");
  evidence.saveStateId = saved.saveStateId;
  await opened.page.close(); evidence.stages.push("product-launch-gamepad-audio-save");
  const restored = await launchCart(client, gameId, saved.saveStateId);
  assert.notEqual(restored.launchId, launch.launchId);
  const resumed = await open(context, restored);
  await resumed.page.waitForTimeout(2000);
  await gamepad(resumed.page, 14, 800); await gamepad(resumed.page, 0, 400);
  evidence.restored = await canvasDigest(resumed.canvas);
  assert.ok(evidence.restored.colors > 4, "PX68K_RESTORED_FRAME_EMPTY");
  await resumed.canvas.screenshot({path: join(directory, "restored.png")});
  await saveCart(resumed.page, restored.launchId, "px68k");
  evidence.stages.push("fresh-launch-restore-input-save");
  await resumed.page.close();
  assert.deepEqual(evidence.errors, []); evidence.status = "PASS";
} catch (error) {
  evidence.errorCode = /^[A-Z][A-Z0-9_]+$/u.test(error.message) ? error.message : "PX68K_ACCEPTANCE_FAILED";
  process.exitCode = 1;
}
finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "px68k-product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}
async function open(context, launch) {
  const page = await context.newPage();
  page.on("pageerror", error => evidence.errors.push(error.message.slice(0, 200)));
  page.on("response", async response => {
    if (!response.url().endsWith(`/runtime/launches/${launch.launchId}/config`) || response.status() !== 200) {return;}
    const envelope = await response.json();
    evidence.runtime = {providerId: envelope.runtime.providerId, targetId: envelope.runtime.targetId,
      bundleSha256: envelope.runtime.bundleSha256, providerVersion: envelope.runtime.providerVersion};
  });
  await page.goto(`${baseUrl}${launch.playUrl}`, {waitUntil: "domcontentloaded", timeout: 60000});
  console.log("PX68K: document loaded");
  try {return {page, canvas: await px68kCanvas(page)};}
  catch (error) {await page.screenshot({path: join(directory, "failed-player.png"), timeout: 10000}); throw error;}
}
