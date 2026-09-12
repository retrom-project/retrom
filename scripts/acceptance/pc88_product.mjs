import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import sharp from "../../web/node_modules/sharp/dist/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, approveCart, launchCart} from "./fantasy_product_client.mjs";
import {installPC88BIOS, importPC88Disk} from "./pc88_product_support.mjs";
import {observePC88Audio, openPC88, pressPC88, savePC88, enterPC88Map} from "./pc88_product_browser.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/pc88-product");
const evidence = {schemaVersion: 1, caseId: "ACC-PC88-001", status: "FAIL", errors: [], stages: [], runtimes: []};
mkdirSync(directory, {recursive: true});
let browser, proxy;
const stage = name => {evidence.stages.push(name); console.log("pc88_stage=" + name);};
const hash = bytes => createHash("sha256").update(bytes).digest("hex");
try {
  if (![base, env.RETROM_PC88_DISC, env.RETROM_PC88_BIOS_DIR, env.RETROM_CHROME_EXECUTABLE,
    env.RETROM_ACCEPTANCE_USERNAME, env.RETROM_ACCEPTANCE_PASSWORD].every(Boolean)) {
    evidence.status = "BLOCKED"; throw Error("PC88_ACCEPTANCE_INPUT_REQUIRED");
  }
  const disk = readFileSync(env.RETROM_PC88_DISC);
  evidence.disk = {sha256: hash(disk), sizeBytes: disk.length};
  assert.equal(evidence.disk.sha256, "e98b5084ee0392351d1c9a1dc49dc07ba2f799d7cc0ca53a186a28786b8a296a", "PC88_EXPECTED_LIBRARIAN_091");
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--enable-unsafe-swiftshader", "--autoplay-policy=no-user-gesture-required"]});
  evidence.chromeVersion = browser.version();
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(30000);
  await installVirtualStandardGamepad(context); await observePC88Audio(context);
  const client = await fantasyClient(context, base);
  await installPC88BIOS(client, env.RETROM_PC88_BIOS_DIR); stage("bios");
  if (env.RETROM_PC88_RESUME_EVIDENCE) {
    const previous = readFileSync(env.RETROM_PC88_RESUME_EVIDENCE);
    const prior = JSON.parse(previous);
    assert.equal(prior.caseId, evidence.caseId);
    assert.deepEqual(prior.disk, evidence.disk);
    for (const name of ["bios", "import", "review-preview", "publish"]) {assert.ok(prior.stages.includes(name));}
    assert.ok(prior.gameId && prior.previewId && prior.runtimes.length >= 2);
    evidence.resume = {evidenceSha256: hash(previous), previousStatus: prior.status};
    evidence.runtimes.push(...prior.runtimes.slice(0, 2));
    Object.assign(evidence, {itemId: prior.itemId, previewId: prior.previewId, gameId: prior.gameId});
    stage("resume-published-evidence");
  } else {
    const itemId = env.RETROM_PC88_REVIEW_ID ?? (await importPC88Disk(client, env.RETROM_PC88_DISC)).itemId;
    evidence.itemId = itemId; stage("import");
    const preview = await previewCart(client, itemId);
    const trial = await openPC88(context, base, preview, evidence);
    await enterPC88Map(trial, picture); await picture(trial, "review-preview");
    evidence.previewId = preview.previewId;
    await trial.page.close(); stage("review-preview");
    evidence.gameId = (await approveCart(client, itemId)).gameId; stage("publish");
  }
  await verifySavedGame(context, client, evidence.gameId);
  assert.deepEqual(evidence.errors, []); evidence.status = "PASS";
} catch (error) {
  evidence.errorCode = error.message.split("\n")[0].slice(0, 300);
  process.exitCode = evidence.status === "BLOCKED" ? 3 : 1;
} finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "pc88-product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}

async function picture(opened, name) {
  // Only host chrome is hidden for image comparison; native pixels are unchanged.
  const png = await opened.canvas.screenshot({style: ".player-pause-overlay,.player-toolbar,.player-toast,.player-debug-panel,.player-controls-hint,.player-emulator-toolbar{visibility:hidden!important}"});
  if (name) {writeFileSync(join(directory, name + ".png"), png);}
  const {data: pixels, info} = await sharp(png).ensureAlpha().raw().toBuffer({resolveWithObject: true});
  let yellow = 0, leftBody = 0, rightBody = 0;
  for (let i = 0; i < pixels.length; i += 4) {
    const x = (i / 4) % info.width, y = Math.floor(i / 4 / info.width);
    if (pixels[i] > 180 && pixels[i + 1] > 180 && pixels[i + 2] < 80) {
      if (x < 1150 && y >= 50 && y < 85) {yellow++;}
      if (y >= 350 && y < 385) {
        if (x >= 200 && x < 250) {leftBody++;}
        if (x >= 328 && x < 378) {rightBody++;}
      }
    }
  }
  const map = await sharp(png).extract({left: 0, top: 170, width: 1140, height: 480}).raw().toBuffer();
  // Visually verified yellow character body: 282 pixels on its occupied tile,
  // at most 32 in the neighbouring terrain. New terrain and encounters can redraw
  // other pixels after movement; every restore still compares the full map.
  const tile = leftBody > 200 && rightBody < 100 ? "left" : rightBody > 200 && leftBody < 100 ? "right" : null;
  return {yellow, mapSha256: hash(map), tile, leftBody, rightBody};
}

async function verifySavedGame(context, client, gameId) {
  const launch = await launchCart(client, gameId);
  const opened = await openPC88(context, base, launch, evidence);
  if (evidence.resume) {
    for (const key of ["providerId", "providerVersion", "targetId", "moduleSha256", "bundleSha256"]) {
      assert.equal(opened.config.runtime[key], evidence.runtimes[0][key], "PC88_RESUME_CANDIDATE_CHANGED");
    }
  }
  await enterPC88Map(opened, picture);
  const initial = await picture(opened, "A-initial");
  assert.equal(initial.tile, "left", "PC88_INITIAL_POSITION_UNKNOWN");
  await opened.page.waitForTimeout(1200);
  assert.equal((await picture(opened)).mapSha256, initial.mapSha256, "PC88_IDLE_MAP_NOT_STABLE");
  await pressPC88(opened, 15, 80);
  const moved = await picture(opened, "B-before-save");
  assert.equal(moved.tile, "right", "PC88_RIGHT_POSITION_FAILED");
  assert.notEqual(moved.mapSha256, initial.mapSha256, "PC88_DIRECTION_FAILED");
  const saved = await savePC88(opened, client, launch.launchId, gameId);
  const screenshot = await client.raw("GET", saved.screenshotUrl);
  assert.equal(screenshot.status(), 200);
  const png = await screenshot.body();
  assert.ok((await sharp(png).removeAlpha().raw().toBuffer()).some(value => value > 100), "PC88_SAVE_SCREENSHOT_EMPTY");
  writeFileSync(join(directory, "uploaded-screenshot.png"), png);
  evidence.checkpoint = {saveStateId: saved.saveStateId, sizeBytes: saved.sizeBytes, originalLaunchId: launch.launchId};
  await pressPC88(opened, 14, 80);
  const later = await picture(opened, "C-later");
  assert.notEqual(later.mapSha256, moved.mapSha256, "PC88_POST_SAVE_DIRECTION_FAILED");
  assert.equal(later.tile, "left", "PC88_LEFT_POSITION_FAILED");
  evidence.audio = await opened.frame.evaluate(() => window.__pc88Audio);
  assert.ok(evidence.audio.nonzeroBuffers > 0, "PC88_AUDIO_SIGNAL_MISSING");
  await opened.page.close(); stage("product-input-save");
  const restored = await launchCart(client, gameId, saved.saveStateId);
  assert.notEqual(restored.launchId, launch.launchId);
  const resumed = await openPC88(context, base, restored, evidence);
  assert.equal(resumed.config.restore.format, "emulatorjs-state-v1-storage-v1");
  await resumed.page.waitForTimeout(1000);
  const restoredMap = await picture(resumed, "B-restored");
  assert.equal(restoredMap.mapSha256, moved.mapSha256, "PC88_EXECUTION_STATE_NOT_RESTORED");
  await pressPC88(resumed, 14, 80);
  const afterInput = await picture(resumed, "C-restored-input");
  assert.equal(afterInput.tile, "left", "PC88_RESTORED_INPUT_FAILED");
  await resumed.page.close();
  // Test the keyboard from the same saved position, independently of any random
  // encounter caused by the previous move. No native state or input is injected.
  const keyboardLaunch = await launchCart(client, gameId, saved.saveStateId);
  const keyed = await openPC88(context, base, keyboardLaunch, evidence);
  await keyed.page.waitForTimeout(1000);
  assert.equal((await picture(keyed)).mapSha256, moved.mapSha256, "PC88_KEYBOARD_START_NOT_RESTORED");
  await keyed.canvas.click(); await keyed.page.keyboard.press("Numpad4", {delay: 80});
  await keyed.page.waitForTimeout(500);
  const keyboard = await picture(keyed, "D-keyboard");
  assert.equal(keyboard.tile, "left", "PC88_KEYBOARD_DIRECTION_FAILED");
  evidence.maps = {initial, moved, later, restored: restoredMap, afterInput, keyboard};
  evidence.checkpoint.restoredLaunchId = restored.launchId;
  evidence.checkpoint.format = resumed.config.restore.format;
  evidence.keyboardLaunchId = keyboardLaunch.launchId;
  await keyed.page.close(); stage("different-launch-restore-input");
}
