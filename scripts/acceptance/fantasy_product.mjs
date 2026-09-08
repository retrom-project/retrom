import assert from "node:assert/strict";
import {mkdirSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {createFantasyFixture, spritePosition, observeFantasyAudio, fantasyAudioEvidence} from "./fantasy_fixture.mjs";
import {fantasyClient, importCart, previewCart, approveCart, launchCart, runtimeCanvas, gamepad, saveCart} from "./fantasy_product_client.mjs";

const core = process.argv[2];
assert.ok(["tic80", "fake08"].includes(core));
const caseId = core === "tic80" ? "ACC-TIC-001" : "ACC-PICO-001";
const directory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? `.artifacts/fantasy/${core}`);
mkdirSync(directory, {recursive: true});
const external = process.env.RETROM_FANTASY_TEST_CART;
const baseUrl = process.env.RETROM_ACCEPTANCE_BASE_URL;
const missing = [baseUrl, external, process.env.RETROM_CHROME_EXECUTABLE,
  process.env.RETROM_ACCEPTANCE_USERNAME, process.env.RETROM_ACCEPTANCE_PASSWORD].some((value) => !value);
const evidence = {schemaVersion: 1, caseId, core, fixtureId: process.env.RETROM_FANTASY_FIXTURE_ID ?? "default", status: "FAIL", errors: [], stages: []};
let browser, proxy;
try {
  if (missing) {evidence.status = "BLOCKED"; throw Error("FANTASY_ACCEPTANCE_INPUT_REQUIRED");}
  proxy = await localRpgAcceptanceProxy(baseUrl);
  browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  const context = await browser.newContext({viewport: {width: 1440, height: 1000}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context);
  await observeFantasyAudio(context);
  context.setDefaultTimeout(15000);
  const client = await fantasyClient(context, baseUrl);
  await verifyOwned(context, client);
  await verifyExternal(context, client);
  assert.equal(evidence.errors.length, 0, "FANTASY_BROWSER_ERRORS");
  evidence.status = "PASS";
} catch (error) {
  evidence.errorCode = error.message;
  process.exitCode = evidence.status === "BLOCKED" ? 3 : 1;
} finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "fantasy-product.json"), JSON.stringify(evidence, null, 2) + "\n");
  process.stdout.write(JSON.stringify(evidence) + "\n");
}
async function open(context, launch, filename) {
  const page = await context.newPage();
  page.on("pageerror", (error) => evidence.errors.push(error.message.slice(0, 300)));
  await page.goto(`${baseUrl}${launch.playUrl}`, {waitUntil: "domcontentloaded", timeout: 60000});
  const canvas = await runtimeCanvas(page, core);
  await canvas.screenshot({path: join(directory, filename)});
  return {page, canvas};
}
async function publish(context, client, filename, label) {
  const review = await importCart(client, core, filename);
  const preview = await previewCart(client, review.itemId);
  const opened = await open(context, preview, `${label}-preview.png`);
  await gamepad(opened.page, 0);
  await opened.page.close();
  const game = await approveCart(client, review.itemId);
  evidence.stages.push(`${label}:import-review-preview-publish`);
  return game.gameId;
}
async function verifyOwned(context, client) {
  const gameId = await publish(context, client, createFantasyFixture(core, directory), "owned");
  const original = await launchCart(client, gameId);
  const {page, canvas} = await open(context, original, "owned-start.png");
  const start = await spritePosition(canvas); assert.equal(start, 20);
  await gamepad(page, 15);
  const moved = await spritePosition(canvas); assert.notEqual(moved, start);
  await gamepad(page, 0); // TIC-80's game writes pmem on confirm.
  const savedPosition = await spritePosition(canvas);
  await canvas.screenshot({path: join(directory, "owned-saved.png")});
  const saved = await saveCart(page, original.launchId, core);
  await page.close();
  const restored = await launchCart(client, gameId, saved.saveStateId);
  assert.notEqual(restored.launchId, original.launchId);
  const resumed = await open(context, restored, "owned-restored.png");
  assert.equal(await spritePosition(resumed.canvas), savedPosition, "FANTASY_RESTORE_POSITION_MISMATCH");
  await gamepad(resumed.page, 14);
  assert.notEqual(await spritePosition(resumed.canvas), savedPosition, "FANTASY_RESTORE_INPUT_FAILED");
  await resumed.page.close();
  const clean = await open(context, await launchCart(client, gameId), "owned-clean-restart.png");
  assert.equal(await spritePosition(clean.canvas), start, "FANTASY_UNREQUESTED_RESTORE");
  await clean.page.close();
  evidence.owned = {gameId, saveStateId: saved.saveStateId, originalLaunchId: original.launchId,
    restoredLaunchId: restored.launchId, savedPosition, checkpointFormat: saved.checkpointFormat};
  evidence.stages.push("owned:gamepad-save-fresh-launch-restore-input-clean-restart");
}
async function verifyExternal(context, client) {
  const gameId = await publish(context, client, external, "external");
  const launch = await launchCart(client, gameId);
  const opened = await open(context, launch, "external-product.png");
  await opened.page.waitForTimeout(2000);
  await gamepad(opened.page, core === "tic80" ? 1 : 0);
  await opened.page.waitForTimeout(2000);
  await gamepad(opened.page, 15);
  await gamepad(opened.page, 0);
  await opened.canvas.screenshot({path: join(directory, "external-after-input.png")});
  const audio = await fantasyAudioEvidence(opened.page);
  assert.ok(audio.nonzeroBuffers > 0, "FANTASY_AUDIO_SIGNAL_MISSING");
  evidence.audio = audio;
  if (core === "fake08") {
    const saved = await saveCart(opened.page, launch.launchId, core);
    await opened.page.close();
    const restoreLaunch = await launchCart(client, gameId, saved.saveStateId);
    evidence.externalRestore = {saveStateId: saved.saveStateId, launchId: restoreLaunch.launchId};
    const restored = await open(context, restoreLaunch, "external-restored.png");
    await gamepad(restored.page, 0); await restored.page.close();
  } else {await opened.page.close();}
  evidence.external = {gameId, launchId: launch.launchId};
  evidence.stages.push("external:product-launch-input" + (core === "fake08" ? "-restore" : ""));
}
