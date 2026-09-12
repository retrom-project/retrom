import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {mkdirSync, readFileSync, writeFileSync, existsSync} from "node:fs";
import {join, resolve} from "node:path";
import {px68kLocalProxy} from "./px68k_product_support.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, approveCart, launchCart} from "./fantasy_product_client.mjs";
import {installMiniBIOS, importMini} from "./pokemini_product_support.mjs";
import {observeMini, openMini, pictureMini, pressMini, pauseMini, saveMini} from "./pokemini_product_browser.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/pokemini-product");
mkdirSync(directory, {recursive: true});
const evidence = {schemaVersion: 1, caseId: "ACC-POKEMINI-001", status: "FAIL", errors: [], stages: [], runtimes: [], diskRequests: 0};
const stage = name => {evidence.stages.push(name); console.log("pokemini_stage=" + name);};
let browser, proxy;
try {
  if (![base, env.RETROM_POKEMINI_ROM, env.RETROM_POKEMINI_BIOS_DIR, env.RETROM_CHROME_EXECUTABLE,
    env.RETROM_ACCEPTANCE_USERNAME, env.RETROM_ACCEPTANCE_PASSWORD].every(Boolean)) {
    evidence.status = "BLOCKED"; throw Error("POKEMINI_ACCEPTANCE_INPUT_REQUIRED");
  }
  const rom = readFileSync(env.RETROM_POKEMINI_ROM);
  evidence.disk = {sha256: createHash("sha256").update(rom).digest("hex"), sizeBytes: rom.length};
  assert.equal(evidence.disk.sha256, "6c473867e9bad1883307d5372dedfa9443a620423d82ae83b6171fcaaffc291e", "POKEMINI_PUZZLE_COLLECTION_REQUIRED");
  const {chromium} = await import(env.RETROM_PLAYWRIGHT_MODULE ?? "../../web/node_modules/playwright/index.mjs");
  proxy = await px68kLocalProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader"]});
  evidence.chromeVersion = browser.version();
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(30000);
  await installVirtualStandardGamepad(context); await observeMini(context);
  const client = await fantasyClient(context, base);
  await installMiniBIOS(client, env.RETROM_POKEMINI_BIOS_DIR);
  const path = join(directory, "product-input.json");
  const progress = existsSync(path) ? JSON.parse(readFileSync(path)) : {digest: evidence.disk.sha256};
  assert.equal(progress.digest, evidence.disk.sha256, "POKEMINI_INPUT_CHANGED");
  progress.review ??= await importMini(client, env.RETROM_POKEMINI_ROM);
  writeFileSync(path, JSON.stringify(progress)); stage("import");
  const preview = await previewCart(client, progress.review.itemId);
  const trial = await openMini(context, base, preview, evidence);
  await boot(trial); await pictureMini(trial, directory, "review-preview");
  await trial.page.close(); stage("review-preview");
  progress.gameId ??= (await approveCart(client, progress.review.itemId)).gameId;
  writeFileSync(path, JSON.stringify(progress)); evidence.gameId = progress.gameId;
  const launch = await launchCart(client, progress.gameId), opened = await openMini(context, base, launch, evidence);
  await boot(opened);
  await pressMini(opened, 0); await opened.page.waitForTimeout(1000);
  const start = await pictureMini(opened, directory, "menu-start");
  await pressMini(opened, 13);
  const moved = await pictureMini(opened, directory, "menu-moved");
  assert.notEqual(moved.contentSha256, start.contentSha256, "POKEMINI_DIRECTION_FAILED");
  await pressMini(opened, 0); await opened.page.waitForTimeout(1000);
  const confirmed = await pictureMini(opened, directory, "confirmed");
  assert.notEqual(confirmed.contentSha256, moved.contentSha256, "POKEMINI_CONFIRM_FAILED");
  await pauseMini(opened);
  const paused = await pictureMini(opened, directory, "saved");
  const saved = await saveMini(opened, client, launch.launchId, progress.gameId, directory);
  evidence.checkpoint = {saveStateId: saved.saveStateId, sizeBytes: saved.sizeBytes, originalLaunchId: launch.launchId};
  evidence.audio = await opened.frame.evaluate(() => window.__miniAudio);
  assert.ok(evidence.audio.varyingBuffers > 0, "POKEMINI_AUDIO_MISSING");
  await opened.page.close(); stage("product-input-save");
  const restore = await launchCart(client, progress.gameId, saved.saveStateId);
  assert.notEqual(restore.launchId, launch.launchId);
  const resumed = await openMini(context, base, restore, evidence);
  assert.equal(resumed.config.restore.format, "gbe-pokemini-state-v1-storage-v1");
  await resumed.page.waitForTimeout(300);
  const restored = await pictureMini(resumed, directory, "restored");
  assert.equal(restored.checkpointSha256, paused.checkpointSha256, "POKEMINI_EXECUTION_STATE_NOT_RESTORED");
  await pressMini(resumed, 0); await resumed.page.waitForTimeout(3000);
  const gameplay = await pictureMini(resumed, directory, "gameplay");
  assert.notEqual(gameplay.checkpointSha256, restored.checkpointSha256, "POKEMINI_LEVEL_START_FAILED");
  await pressMini(resumed, 15); await pressMini(resumed, 0);
  const afterInput = await pictureMini(resumed, directory, "restored-input");
  assert.notEqual(afterInput.contentSha256, gameplay.contentSha256, "POKEMINI_RESTORED_INPUT_FAILED");
  evidence.checkpoint.restoredLaunchId = restore.launchId;
  evidence.pictures = {start, moved, confirmed, paused, restored, afterInput, gameplay};
  assert.equal(evidence.diskRequests, 1, "POKEMINI_ROM_CACHE_MISS");
  await resumed.page.close(); stage("different-launch-restore-input-cache");
  assert.deepEqual(evidence.errors, []); evidence.status = "PASS";
} catch (error) {
  console.log("pokemini_error=" + error.message.split("\n")[0]);
  evidence.errorCode = error.message.split("\n")[0].slice(0, 300);
  process.exitCode = evidence.status === "BLOCKED" ? 3 : 1;
} finally {
  if (evidence.status === "FAIL") {
    for (const [index, page] of (browser?.contexts().flatMap(context => context.pages()) ?? []).entries()) {
      await page.screenshot({path: join(directory, `failure-${index}.png`), timeout: 5000}).catch(() => undefined);
    }
  }
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "pokemini-product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}
async function boot(opened) {
  await opened.frame.waitForFunction(() => window.__miniFrames() >= 650, undefined, {timeout: 60000});
  assert.ok((await pictureMini(opened)).colors > 1, "POKEMINI_BOOT_EMPTY");
  // Fresh EEPROM starts in the Mini BIOS clock setup. Confirm its six fields
  // through the standard controller, then wait for Puzzle Collection's title.
  for (let i = 0; i < 6; i++) {await pressMini(opened, 0);}
  // Fingerprint only the title's monochrome header. This waits through the
  // copyright fade without shipping ROM pixels or confusing animation with input.
  const deadline = Date.now() + 60000;
  while (Date.now() < deadline) {
    if ((await pictureMini(opened)).headerSha256 === "08d507151a86c0cdeeb7d011e4ade6bdcdf2dbb7dd6187da46657bb1fbe0f77d") {return;}
    await opened.page.waitForTimeout(100);
  }
  throw Error("POKEMINI_TITLE_TIMEOUT");
}
