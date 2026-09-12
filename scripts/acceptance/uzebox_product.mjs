import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import sharp from "../../web/node_modules/sharp/dist/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, approveCart, launchCart} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {openUzebox, pressUzebox, saveUzebox, observeUzeboxAudio} from "./uzebox_browser.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/uzebox-product");
const evidence = {schemaVersion: 1, caseId: "ACC-UZEBOX-001", status: "FAIL", errors: [], stages: [], runtimes: []};
mkdirSync(directory, {recursive: true});
let browser, proxy;
const stage = name => {evidence.stages.push(name); console.log("uzebox_stage=" + name);};
const hash = bytes => createHash("sha256").update(bytes).digest("hex");
try {
  if (![base, env.RETROM_UZEBOX_ROM, env.RETROM_UZEBOX_CORE_SHA256, env.RETROM_CHROME_EXECUTABLE,
    env.RETROM_ACCEPTANCE_USERNAME, env.RETROM_ACCEPTANCE_PASSWORD].every(Boolean)) {
    evidence.status = "BLOCKED"; throw Error("UZEBOX_ACCEPTANCE_INPUT_REQUIRED");
  }
  const cart = readFileSync(env.RETROM_UZEBOX_ROM);
  evidence.cart = {sha256: hash(cart), sizeBytes: cart.length};
  assert.equal(evidence.cart.sha256, "4f398029fac2f2b854df49df632b75b2148e034d331c7c4be41a78f97232a62a");
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--enable-unsafe-swiftshader", "--autoplay-policy=no-user-gesture-required"]});
  evidence.chromeVersion = browser.version();
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(30000);
  await installVirtualStandardGamepad(context); await observeUzeboxAudio(context);
  const client = await fantasyClient(context, base);
  evidence.itemId = env.RETROM_UZEBOX_REVIEW_ID ?? (await importGame(client)).itemId; stage("import");
  const preview = await previewCart(client, evidence.itemId);
  const trial = await openUzebox(context, base, preview, evidence);
  await trial.page.waitForTimeout(3500); await picture(trial, "review-preview");
  evidence.previewId = preview.previewId;
  await trial.page.close(); stage("review-preview");
  evidence.gameId = (await approveCart(client, evidence.itemId)).gameId; stage("publish");
  await verifyGame(context, client);
  assert.deepEqual(evidence.errors, []); evidence.status = "PASS";
} catch (error) {
  evidence.errorCode = error.message.split("\n")[0].slice(0, 300);
  process.exitCode = evidence.status === "BLOCKED" ? 3 : 1;
} finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "uzebox-product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}
async function importGame(client) {
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  const platforms = await client.json("GET", "/api/v1/admin/platform-instances?platformId=uzebox&limit=100");
  const instance = platforms.items.find(item => item.enabled && item.defaultCoreId === "uzem");
  assert.ok(instance, "UZEBOX_PLATFORM_MISSING");
  const uploadId = await client.upload(singleFile(env.RETROM_UZEBOX_ROM), "FILES", "GENERAL");
  const imported = await client.json("POST", "/api/v1/admin/imports", {
    headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []},
  });
  return reviewForImport(client, imported.importJobId);
}
async function picture(opened, name) {
  const png = await opened.canvas.screenshot({style: ".player-pause-overlay,.player-toolbar,.player-toast,.player-debug-panel,.player-controls-hint,.player-emulator-toolbar,nextjs-portal{visibility:hidden!important}"});
  if (name) {writeFileSync(join(directory, name + ".png"), png);}
  const {data, info} = await sharp(png).resize(720, 224, {fit: "fill"}).removeAlpha().raw().toBuffer({resolveWithObject: true});
  assert.ok(data.some(value => value > 100), "UZEBOX_BLANK_FRAME");
  const xs = [];
  for (let y = 201; y < 209; y++) {
    for (let x = 40; x < 680; x++) {
      const offset = (y * info.width + x) * info.channels;
      if (data[offset] > 180 && data[offset + 1] > 25 && data[offset + 1] < 130 && data[offset + 2] < 65) {
        xs.push(x);
      }
    }
  }
  let ballMinY = null;
  for (let y = 85; y < 200; y++) {
    for (let x = 40; x < 680; x++) {
      const offset = (y * info.width + x) * info.channels;
      if (data[offset] < 100 && data[offset + 1] > 200 && data[offset + 2] > 200) {
        ballMinY = ballMinY === null ? y : Math.min(ballMinY, y);
      }
    }
  }
  return {sha256: hash(data), ballMinY, paddleX: xs.length > 25 ? (Math.min(...xs) + Math.max(...xs)) / 2 : null};
}
async function verifyGame(context, client) {
  const launch = await launchCart(client, evidence.gameId);
  const opened = await openUzebox(context, base, launch, evidence);
  await opened.page.waitForTimeout(3500); await picture(opened, "title");
  await pressUzebox(opened, 9);
  let ready = false;
  for (let attempt = 0; attempt < 40; attempt++) {
    await opened.page.waitForTimeout(500);
    if ((await picture(opened)).paddleX !== null) {ready = true; break;}
  }
  assert.ok(ready, "UZEBOX_START_OR_READY_FAILED");
  await opened.page.waitForTimeout(1000);
  const initial = await picture(opened, "A-initial");
  await pressUzebox(opened, 15, 300);
  const moved = await picture(opened, "B-before-save");
  assert.ok(moved.paddleX > initial.paddleX + 20, "UZEBOX_DIRECTION_FAILED");
  evidence.positions = {initial, moved};
  const saved = await saveUzebox(opened, client, launch.launchId, evidence.gameId);
  evidence.checkpoint = {saveStateId: saved.saveStateId, sizeBytes: saved.sizeBytes, originalLaunchId: launch.launchId};
  const response = await client.raw("GET", saved.screenshotUrl);
  assert.equal(response.status(), 200); writeFileSync(join(directory, "uploaded-screenshot.png"), await response.body());
  await pressUzebox(opened, 14, 300);
  const later = await picture(opened, "C-later");
  assert.ok(later.paddleX < moved.paddleX - 20, "UZEBOX_LEFT_FAILED");
  evidence.positions.later = later;
  evidence.audio = await opened.frame.evaluate(() => window.__uzeboxAudio);
  assert.ok(evidence.audio.nonzeroBuffers > 0, "UZEBOX_AUDIO_MISSING");
  await opened.page.close(); stage("product-input-save");
  const restoredLaunch = await launchCart(client, evidence.gameId, saved.saveStateId);
  assert.notEqual(restoredLaunch.launchId, launch.launchId);
  const restored = await openUzebox(context, base, restoredLaunch, evidence);
  let position = await picture(restored, "restored-B");
  evidence.positions.restoredFirst = position;
  assert.equal(position.paddleX, moved.paddleX, "UZEBOX_RESTORE_POSITION_MISMATCH");
  const matchStarted = Date.now(); let samples = 1;
  // The native machine resumes immediately, including the 120-frame tail-light cycle.
  // Keep exact whole-frame equality and bound the wait for its saved visual phase.
  while (position.sha256 !== moved.sha256 && Date.now() - matchStarted < 10000) {
    await restored.page.waitForTimeout(100);
    position = await picture(restored, "restored-B"); samples++;
  }
  evidence.visualPhaseMatch = {samples, elapsedMs: Date.now() - matchStarted};
  evidence.positions.restored = position;
  assert.equal(position.sha256, moved.sha256, "UZEBOX_RESTORE_FRAME_MISMATCH");
  await pressUzebox(restored, 14, 300);
  const afterInput = await picture(restored, "restored-input");
  assert.ok(afterInput.paddleX < position.paddleX - 20, "UZEBOX_RESTORED_INPUT_FAILED");
  await pressUzebox(restored, 0);
  await restored.page.waitForTimeout(500);
  const playing = await picture(restored, "restored-confirm");
  assert.ok(playing.ballMinY !== null && playing.ballMinY < 185, "UZEBOX_CONFIRM_FAILED");
  evidence.positions.afterInput = afterInput; evidence.positions.playing = playing;
  await restored.page.close(); stage("fresh-launch-restore-input");
}
