import assert from "node:assert/strict";
import {observeContentIO, contentSourceMatcher} from "./content_io_observation.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {finalContentMetrics} from "./content_io_measurement.mjs";
import {validateContentResources} from "./content_io_performance.mjs";
import {exitContentIOPlayer, performContentIOPlayerExit} from "./content_io_player_exit.mjs";
import {withFantasyRunCart} from "./fantasy_run_cart.mjs";
import {copyFileSync, mkdirSync, writeFileSync} from "node:fs";
import {fileURLToPath} from "node:url";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {spritePosition, spriteState, observeFantasyAudio, fantasyAudioEvidence} from "./fantasy_fixture.mjs";
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
const evidence = {schemaVersion: 1, caseId, core, runId: process.env.RETROM_CONTENT_IO_RUN_ID ?? null, status: "FAIL", errors: [], stages: []};
let browser, proxy, collector;
try {
  if (missing) {evidence.status = "BLOCKED"; throw Error("FANTASY_ACCEPTANCE_INPUT_REQUIRED");}
  proxy = await localRpgAcceptanceProxy(baseUrl);
  browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  const context = await browser.newContext({viewport: {width: 1440, height: 1000}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context);
  await observeFantasyAudio(context);
  collector = await observeContentStoreEvents(context, {retain: true});
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
if (process.env.RETROM_CONTENT_IO_RUN_ID && evidence.status === "PASS") {
  try {
    const {completeFantasyContentProof} = await import("./fantasy_content_proof.mjs");
    await completeFantasyContentProof(directory, evidence);
  } catch (error) {console.error(error.message); process.exitCode = 1;}
}
async function open(context, launch, filename) {
  const response = await context.request.get(`${baseUrl}/runtime/launches/${launch.launchId ?? launch.previewId}/config`);
  assert.equal(response.status(), 200); const config = await response.json();
  assert.equal(config.runtime.targetId, core);
  (evidence.runtimes ??= []).push({bundleSha256: config.runtime.bundleSha256, moduleSha256: config.runtime.moduleSha256});
  const observer = observeContentIO(context, contentSourceMatcher(config.resources.filter(row => row.role === "game"), baseUrl));
  await observer.ready;
  const page = await context.newPage();
  page.on("pageerror", (error) => evidence.errors.push(error.message.slice(0, 300)));
  await page.goto(`${baseUrl}${launch.playUrl}`, {waitUntil: "domcontentloaded", timeout: 60000});
  const canvas = await runtimeCanvas(page, core);
  await canvas.screenshot({path: join(directory, filename)});
  return {page, canvas, launch, observer};
}
async function publish(context, client, filename, label) {
  const review = await importCart(client, core, filename);
  const preview = await previewCart(client, review.itemId);
  const opened = await open(context, preview, `${label}-preview.png`);
  if (label === "owned") evidence.previewState = await spriteState(opened.canvas);
  await gamepad(opened.page, 0);
  await opened.observer.flush();
  if (label === "owned") {
    const requests = opened.observer.requests.filter(row => row.method !== "HEAD");
    assert.equal(requests.length, 1); assert.equal(requests[0].status, 200);
    assert.equal(requests[0].sizeBytes, evidence.ownedSource.outputSizeBytes);
    evidence.coldCartRequests = requests.length; evidence.coldCartBytes = requests[0].sizeBytes;
  }
  opened.observer.close(); await opened.page.close();
  const game = await approveCart(client, review.itemId);
  evidence.stages.push(`${label}:import-review-preview-publish`);
  return game.gameId;
}
async function verifyOwned(context, client) {
  const extension = core === "tic80" ? "tic" : "p8";
  const fixture = fileURLToPath(new URL(`../../testdata/public-roms/fantasy-controls/controls.${extension}`, import.meta.url));
  const gameId = await withFantasyRunCart(core, fixture, (filename, receipt) => {
    evidence.ownedSource = receipt; copyFileSync(filename, join(directory, `retrom-checkpoint.${extension}`));
    return publish(context, client, filename, "owned");
  });
  const original = await launchCart(client, gameId);
  const first = await open(context, original, "owned-start.png"), {page, canvas} = first;
  const initialState = await spriteState(canvas), start = initialState.x; assert.equal(start, 20);
  await gamepad(page, 15);
  const moved = await spritePosition(canvas); assert.notEqual(moved, start);
  await gamepad(page, 0); // TIC-80's game writes pmem on confirm.
  const savedState = await spriteState(canvas), savedPosition = savedState.x;
  assert.notDeepEqual(savedState.color, initialState.color, "FANTASY_CONFIRM_NOT_VISIBLE");
  await canvas.screenshot({path: join(directory, "owned-saved.png")});
  const saved = await saveAndClose(first, gameId);
  const restored = await launchCart(client, gameId, saved.saveStateId);
  assert.notEqual(restored.launchId, original.launchId);
  const resumed = await open(context, restored, "owned-restored.png");
  const restoredState = await spriteState(resumed.canvas);
  assert.deepEqual(restoredState, savedState, "FANTASY_RESTORE_STATE_MISMATCH");
  await gamepad(resumed.page, 14);
  const restoredInputPosition = await spritePosition(resumed.canvas);
  assert.ok(restoredInputPosition < savedPosition, "FANTASY_RESTORE_INPUT_FAILED");
  await resumed.observer.flush(); assert.equal(resumed.observer.requests.length, 0, "FANTASY_WARM_CART_NETWORK");
  evidence.warmCartRequests = resumed.observer.requests.length;
  await closeProduct(resumed, gameId);
  const clean = await open(context, await launchCart(client, gameId), "owned-clean-restart.png");
  assert.equal(await spritePosition(clean.canvas), start, "FANTASY_UNREQUESTED_RESTORE");
  await closeProduct(clean, gameId);
  evidence.owned = {gameId, saveStateId: saved.saveStateId, originalLaunchId: original.launchId,
    restoredLaunchId: restored.launchId, initialState, savedState, restoredState, savedPosition, restoredInputPosition, checkpointFormat: saved.checkpointFormat};
  evidence.stages.push("owned:gamepad-save-fresh-launch-restore-input-clean-restart");
}
async function verifyExternal(context, client) {
  const gameId = await withFantasyRunCart(core, external, async (filename, receipt) => {
    evidence.externalSource = receipt; return publish(context, client, filename, "external");
  });
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
    const saved = await saveAndClose(opened, gameId);
    const restoreLaunch = await launchCart(client, gameId, saved.saveStateId);
    evidence.externalRestore = {saveStateId: saved.saveStateId, launchId: restoreLaunch.launchId};
    const restored = await open(context, restoreLaunch, "external-restored.png");
    await gamepad(restored.page, 0); await closeProduct(restored, gameId);
  } else {await closeProduct(opened, gameId);}
  evidence.external = {gameId, launchId: launch.launchId};
  evidence.stages.push("external:product-launch-input" + (core === "fake08" ? "-restore" : ""));
}

async function collectExit(opened) {
  await opened.observer.flush(); const sessions = collector.snapshot(opened.page), metrics = finalContentMetrics(sessions);
  validateContentResources(metrics.publicPeak, false); validateContentResources(metrics.closed, true);
  (evidence.contentIO ??= []).push({launchId: opened.launch.launchId, sessions, gameRequests: opened.observer.requests});
  opened.observer.close(); await opened.page.close();
}
async function closeProduct(opened, gameId) {
  await exitContentIOPlayer(opened.page, baseUrl, {...opened.launch, returnTo: `/games/${gameId}`}, core === "tic80" ? "GAME_SAVE" : "INSTANT");
  await collectExit(opened);
}
async function saveAndClose(opened, gameId) {
  const save = () => saveCart(opened.page, opened.launch.launchId, core);
  if (core === "tic80") {
    const saved = await performContentIOPlayerExit(opened.page, baseUrl, {...opened.launch, returnTo: `/games/${gameId}`}, save);
    await collectExit(opened); return saved;
  }
  const saved = await save(); await closeProduct(opened, gameId); return saved;
}
