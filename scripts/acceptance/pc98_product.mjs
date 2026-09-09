import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, approveCart, launchCart} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {observePC98, openPC98, menuPC98, picturePC98, visiblePC98Menu, pressPC98, pausePC98, savePC98} from "./pc98_product_browser.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/pc98-product");
const evidence = {schemaVersion: 1, caseId: "ACC-PC98-001", status: "FAIL", errors: [], stages: [], runtimes: [], diskRequests: 0};
mkdirSync(directory, {recursive: true});
let browser, proxy;
const stage = name => {evidence.stages.push(name); console.log("pc98_stage=" + name);};
try {
  if (![base, env.RETROM_PC98_DISC, env.RETROM_CHROME_EXECUTABLE,
    env.RETROM_ACCEPTANCE_USERNAME, env.RETROM_ACCEPTANCE_PASSWORD].every(Boolean)) {
    evidence.status = "BLOCKED"; throw Error("PC98_ACCEPTANCE_INPUT_REQUIRED");
  }
  const disk = readFileSync(env.RETROM_PC98_DISC);
  evidence.disk = {sha256: createHash("sha256").update(disk).digest("hex"), sizeBytes: disk.length};
  assert.equal(evidence.disk.sha256, "3bc33e01942b253cef0e19ab2ce6cf4c27befe49a0f4c1e5a021711480c100da", "PC98_EXPECTED_PERET_EM_HERU_DISC");
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required"]});
  evidence.chromeVersion = browser.version();
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(30000);
  await installVirtualStandardGamepad(context); await observePC98(context);
  const client = await fantasyClient(context, base);
  const itemId = await importPC98(client);
  evidence.itemId = itemId; stage("import");
  const preview = await previewCart(client, itemId);
  evidence.previewId = preview.previewId;
  const trial = await openPC98(context, base, preview, evidence);
  await menuPC98(trial); await picturePC98(trial, directory, "review-preview");
  evidence.progress = await trial.page.evaluate(() => window.__pc98Progress);
  assert.ok(evidence.progress.some(value => value > 0 && value < 100), "PC98_VISIBLE_DOWNLOAD_PROGRESS_MISSING");
  assert.equal(evidence.diskRequests, 1, "PC98_FIRST_DISK_DOWNLOAD_COUNT");
  await trial.page.close(); stage("review-preview");
  const published = await approveCart(client, itemId); evidence.gameId = published.gameId; stage("publish");
  await verifySavedGame(context, client, published.gameId);
  assert.deepEqual(evidence.errors, []); evidence.status = "PASS";
} catch (error) {
  evidence.errorCode = error.message.split("\n")[0].slice(0, 300);
  process.exitCode = evidence.status === "BLOCKED" ? 3 : 1;
} finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "pc98-product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}

async function importPC98(client) {
  if (env.RETROM_PC98_REVIEW_ID) {return env.RETROM_PC98_REVIEW_ID;}
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {headers: client.writeHeaders(), data: {}, expected: 200});
  const platforms = await client.json("GET", "/api/v1/admin/platform-instances?platformId=pc98&limit=100");
  const instance = platforms.items.find(item => item.enabled && item.defaultCoreId === "np2kai");
  assert.ok(instance, "PC98_PLATFORM_MISSING");
  const uploadId = await client.upload(singleFile(env.RETROM_PC98_DISC), "FILES", "GENERAL");
  const imported = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []}});
  return (await reviewForImport(client, imported.importJobId)).itemId;
}

async function verifySavedGame(context, client, gameId) {
  const launch = await launchCart(client, gameId), opened = await openPC98(context, base, launch, evidence);
  await menuPC98(opened);
  const start = await visiblePC98Menu(opened);
  for (let i = 0; i < 4; i++) {await pressPC98(opened, 13);}
  const moved = await visiblePC98Menu(opened);
  assert.notEqual(moved.menuSha256, start.menuSha256, "PC98_DIRECTION_FAILED");
  await pressPC98(opened, 0);
  assert.notEqual((await picturePC98(opened)).menuSha256, moved.menuSha256, "PC98_CONFIRM_FAILED");
  await pressPC98(opened, 1);
  assert.equal((await visiblePC98Menu(opened)).menuSha256, moved.menuSha256, "PC98_CANCEL_FAILED");
  const pausedFrame = await pausePC98(opened), picture = await picturePC98(opened, directory, "saved");
  assert.ok(picture.blue > 10000, "PC98_PAUSED_SCREENSHOT_EMPTY");
  const saved = await savePC98(opened, client, launch.launchId, gameId, directory);
  const audio = await opened.frame.evaluate(() => window.__pc98Audio);
  assert.ok(audio.nonzeroBuffers > 0, "PC98_AUDIO_SIGNAL_MISSING"); evidence.audio = audio;
  evidence.checkpoint = {saveStateId: saved.saveStateId, sizeBytes: saved.sizeBytes, originalLaunchId: launch.launchId, pausedFrame};
  await opened.page.close(); stage("product-input-save");
  const restored = await launchCart(client, gameId, saved.saveStateId);
  assert.notEqual(restored.launchId, launch.launchId);
  const resumed = await openPC98(context, base, restored, evidence);
  assert.equal(resumed.config.restore.format, "np2kai-state-v1-storage-v1");
  evidence.checkpoint.format = resumed.config.restore.format;
  await resumed.page.waitForTimeout(500);
  const restoredMenu = await visiblePC98Menu(resumed, directory, "restored");
  assert.equal(restoredMenu.menuSha256, moved.menuSha256, "PC98_EXECUTION_STATE_NOT_RESTORED");
  await pressPC98(resumed, 12);
  const afterInput = await visiblePC98Menu(resumed, directory, "restored-input");
  assert.notEqual(afterInput.menuSha256, moved.menuSha256, "PC98_RESTORED_INPUT_FAILED");
  evidence.menu = {start, moved, restored: restoredMenu, afterInput};
  await resumed.canvas.press("Escape", {delay: 80});
  await resumed.page.waitForTimeout(500);
  assert.ok((await picturePC98(resumed)).blue < 10000, "PC98_KEYBOARD_CANCEL_FAILED");
  await resumed.canvas.press("Escape", {delay: 80});
  await resumed.page.waitForTimeout(500);
  assert.ok((await picturePC98(resumed)).blue > 10000, "PC98_KEYBOARD_MENU_FAILED");
  evidence.checkpoint.restoredLaunchId = restored.launchId;
  assert.equal(evidence.diskRequests, 1, "PC98_DISK_CACHE_MISS");
  evidence.cache = {instances: 3, diskRequests: evidence.diskRequests};
  await resumed.page.close(); stage("different-launch-restore-input-cache");
}
