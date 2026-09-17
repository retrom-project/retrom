import assert from "node:assert/strict";
import {existsSync, mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {gunzipSync} from "node:zlib";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, approveCart, launchCart} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {hash, openBBKRPG, pictureBBKRPG, pressBBKRPG, keyboardBBKRPG, mainMenuBBKRPG, saveBBKRPG, restoredBBKRPGPicture} from "./bbkrpg_browser.mjs";

import {installAudioObservation, readAudioObservation} from "./rpgmaker_audio_observation.mjs";
import {revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";
import {checkBBKRPGAudio, measureBBKRPGFrames} from "./bbkrpg_audio.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/bbkrpg-product");
mkdirSync(join(directory, "screenshots"), {recursive: true});
const evidence = {schemaVersion: 1, caseId: "ACC-BBKRPG-001", status: "FAIL",
  stages: [], errors: [], runtimes: [], inputDevice: "virtual-standard-gamepad", audio: {}};
const stage = name => {evidence.stages.push(name); console.log("bbkrpg_stage=" + name);};
let browser, proxy;
try {
  if (![base, env.RETROM_BBKRPG_ROM, env.RETROM_BBKRPG_BIOS_DIR, env.RETROM_BBKRPG_CORE_SHA256,
    env.RETROM_CHROME_EXECUTABLE, env.RETROM_ACCEPTANCE_USERNAME, env.RETROM_ACCEPTANCE_PASSWORD].every(Boolean)) {
    evidence.status = "BLOCKED"; throw Error("BBKRPG_ACCEPTANCE_INPUT_REQUIRED");
  }
  const cart = readFileSync(env.RETROM_BBKRPG_ROM);
  evidence.cart = {sha256: hash(cart), sizeBytes: cart.length};
  assert.equal(evidence.cart.sha256, "1065a3fe123f74341cf27464fd9cc06b444ad58c7f4c96ed67fa50c7d5c380f3");
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--enable-unsafe-swiftshader", "--autoplay-policy=no-user-gesture-required"]});
  evidence.chromeVersion = browser.version();
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(30000);
  await installVirtualStandardGamepad(context);
  await context.addInitScript(installAudioObservation);
  const client = await fantasyClient(context, base);
  await installBIOS(client); stage("bios");
  const work = env.RETROM_ACCEPTANCE_RUN_DIR ? join(env.RETROM_ACCEPTANCE_RUN_DIR, "work") : directory;
  mkdirSync(work, {recursive: true});
  const progressPath = join(work, "bbkrpg-product-input.json");
  const progress = existsSync(progressPath) ? JSON.parse(readFileSync(progressPath)) : {};
  if (progress.cart) {assert.deepEqual(progress.cart, evidence.cart);}
  if (!progress.reviewId) {
    progress.reviewId = (await importGame(client)).itemId;
    progress.cart = evidence.cart;
    writeFileSync(progressPath, JSON.stringify(progress));
  }
  stage("import");
  if (!progress.gameId) {
    const preview = await openBBKRPG(context, base, await previewCart(client, progress.reviewId), evidence, directory);
    await mainMenuBBKRPG(preview, directory);
    evidence.audio.preview = await readAudioObservation(preview.page);
    await pictureBBKRPG(preview, directory, "review-preview");
    await preview.page.close();
    progress.previewCoreSha256 = env.RETROM_BBKRPG_CORE_SHA256;
    progress.preview = {runtime: evidence.runtimes.at(-1), audio: evidence.audio.preview};
    progress.gameId = (await approveCart(client, progress.reviewId)).gameId;
    writeFileSync(progressPath, JSON.stringify(progress));
  }
  assert.equal(progress.previewCoreSha256, env.RETROM_BBKRPG_CORE_SHA256, "BBKRPG_PREVIEW_CORE_CHANGED");
  if (progress.preview) {evidence.reviewPreview = progress.preview;}
  evidence.reviewId = progress.reviewId;
  evidence.gameId = progress.gameId;
  stage("review-preview-and-publish");
  await verifyGame(context, client, progress.gameId);
  assert.deepEqual(evidence.errors, []);
  evidence.status = "PASS";
} catch (error) {
  evidence.errorCode = error.message.split("\n")[0].slice(0, 300);
  writeFileSync(join(directory, "bbkrpg-failure.txt"), String(error.stack));
  process.exitCode = evidence.status === "BLOCKED" ? 3 : 1;
} finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "bbkrpg-product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}

async function installBIOS(client) {
  const requirements = (await client.json("GET", "/api/v1/admin/bios?scope=FULL_CATALOG&coreId=gam4980&limit=100"))
    .items.filter(item => item.coreId === "gam4980");
  assert.deepEqual(requirements.map(item => item.logicalName).sort(), ["8.BIN", "E.BIN"]);
  for (const item of requirements) {
    if (item.status === "MATCHED") {continue;}
    assert.equal(item.activeInstallation, null, "BBKRPG_ACCEPTANCE_WILL_NOT_REPLACE_BIOS");
    const uploadId = await client.upload(singleFile(join(env.RETROM_BBKRPG_BIOS_DIR, item.logicalName)), "FILES", "GENERAL");
    const upload = await client.json("GET", "/api/v1/admin/uploads/" + uploadId);
    await client.json("POST", "/api/v1/admin/bios/" + item.id + "/installations", {expected: 201,
      headers: {...client.writeHeaders(), "If-Match": '"v' + item.version + '"'},
      data: {uploadFileId: upload.files[0].fileId}});
  }
  const installed = (await client.json("GET", "/api/v1/admin/bios?scope=FULL_CATALOG&coreId=gam4980&limit=100"))
    .items.filter(item => item.coreId === "gam4980");
  assert.ok(installed.every(item => item.status === "MATCHED"), "BBKRPG_BIOS_HASH_MISMATCH");
}

async function importGame(client) {
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200});
  const platforms = await client.json("GET", "/api/v1/admin/platform-instances?platformId=bbkrpg&limit=100");
  const instance = platforms.items.find(item => item.enabled && item.defaultCoreId === "gam4980");
  assert.ok(instance, "BBKRPG_PLATFORM_MISSING");
  const uploadId = await client.upload(singleFile(env.RETROM_BBKRPG_ROM), "FILES", "GENERAL");
  const imported = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []}});
  return reviewForImport(client, imported.importJobId);
}

async function verifyGame(context, client, gameId) {
  const firstLaunch = await launchCart(client, gameId);
  const first = await openBBKRPG(context, base, firstLaunch, evidence, directory);
  const initial = await mainMenuBBKRPG(first, directory);
  if (initial.selected !== "new") {await pressBBKRPG(first, 12);}
  const a = await pictureBBKRPG(first, directory, "A-new-journey");
  assert.equal(a.selected, "new");
  await pressBBKRPG(first, 13);
  // Freeze B before observing it: the menu cursor animates while running.
  await revealPreviewToolbar(first.page);
  await first.page.getByRole("button", {name: "暂停", exact: true}).click();
  const b = await pictureBBKRPG(first, directory, "B-load-journey");
  assert.equal(b.selected, "load", "BBKRPG_DIRECTION_FAILED");
  assert.notEqual(a.sha256, b.sha256);
  const {saved, before, native} = await saveBBKRPG(first, client, firstLaunch.launchId, gameId, directory);
  assert.equal(before.sha256, b.sha256);
  assert.ok(native?.sizeBytes > 0, "BBKRPG_NATIVE_SAVE_NOT_OBSERVED");
  await pressBBKRPG(first, 12);
  const c = await pictureBBKRPG(first, directory, "C-after-save");
  assert.equal(c.selected, "new");
  assert.notEqual(c.sha256, b.sha256);
  await first.page.close();
  const restoredLaunch = await launchCart(client, gameId, saved.saveStateId);
  assert.notEqual(restoredLaunch.launchId, firstLaunch.launchId);
  const restored = await openBBKRPG(context, base, restoredLaunch, evidence, directory);
  assert.equal(restored.config.restore.format, "gam4980-state-v2-storage-v1");
  const response = await client.raw("GET", restored.config.restore.url);
  assert.equal(response.status(), 200);
  const stored = await response.body();
  assert.equal(stored.length, saved.sizeBytes);
  assert.equal(hash(stored), restored.config.restore.sha256);
  const decoded = gunzipSync(stored, {maxOutputLength: 16 * 1024 * 1024});
  assert.equal(decoded.length, native.sizeBytes);
  assert.equal(hash(decoded), native.sha256, "BBKRPG_STORAGE_CHANGED_NATIVE_STATE");
  const d = await restoredBBKRPGPicture(restored, directory, b);
  assert.equal(d.selected, "load");
  assert.equal(d.sha256, b.sha256, "BBKRPG_DID_NOT_RESTORE_SELECTED_MENU");
  await keyboardBBKRPG(restored, "w");
  assert.equal((await pictureBBKRPG(restored, directory, "restored-keyboard-input")).selected, "new");
  await pressBBKRPG(restored, 0);
  let journey;
  for (let attempt = 0; attempt < 20; attempt++) {
    journey = await pictureBBKRPG(restored, directory, "restored-confirm-new-journey");
    if (!journey.blank && journey.selected === null) {break;}
    await restored.page.waitForTimeout(500);
  }
  assert.equal(journey.blank, false, "BBKRPG_RESTORED_GAMEPLAY_BLANK");
  assert.equal(journey.selected, null, "BBKRPG_RESTORED_CONFIRM_FAILED");
  assert.notEqual(journey.sha256, a.sha256);
  evidence.checkpoint = {saveStateId: saved.saveStateId, originalLaunchId: firstLaunch.launchId,
    restoredLaunchId: restoredLaunch.launchId, storedBytes: stored.length, nativeBytes: decoded.length,
    nativeSha256: native.sha256, storageSha256: hash(stored)};
  evidence.frames = {a, b, c, d, journey};
  evidence.audio.restored = await readAudioObservation(restored.page);
  evidence.audio.controls = await checkBBKRPGAudio(restored);
  evidence.performance = await measureBBKRPGFrames(restored);
  assert.ok(evidence.performance.fps >= 55, "BBKRPG_FRAME_RATE_TOO_LOW");
  await restored.page.close(); stage("gamepad-save-continue-new-launch-restore-keyboard-confirm");
}
