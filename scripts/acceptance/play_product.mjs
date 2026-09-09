import assert from "node:assert/strict";
import {mkdirSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, launchCart, saveCart} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {observeFantasyAudio, fantasyAudioEvidence} from "./fantasy_fixture.mjs";
import {observePlay, openPlay, playFrames, pausePlay, pressPlay, capturePlay, namePixels, reachPlayFrame} from "./play_product_browser.mjs";

const env = process.env;
const base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/play-product");
const gameId = env.RETROM_PLAY_GAME_ID, seedSave = env.RETROM_PLAY_SAVE_ID;
const evidence = {schemaVersion: 1, caseId: "ACC-PS2-001", status: "FAIL", errors: [], launches: [], rangeRequests: 0, rangeBytes: 0};
mkdirSync(directory, {recursive: true});
let browser, proxy;
try {
  if (![base, gameId, seedSave, env.RETROM_PLAY_PREVIEW_DISC, env.RETROM_CHROME_EXECUTABLE,
    env.RETROM_ACCEPTANCE_USERNAME, env.RETROM_ACCEPTANCE_PASSWORD].every(Boolean)) {
    evidence.status = "BLOCKED"; throw Error("PLAY_ACCEPTANCE_INPUT_REQUIRED");
  }
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context); await observeFantasyAudio(context); await observePlay(context);
  const client = await fantasyClient(context, base);
  await verifyPreview(context, client);
  await verifyCheckpoint(context, client);
  await verifyCache(context, client);
  assert.deepEqual(evidence.errors, []);
  evidence.status = "PASS";
} catch (error) {
  evidence.errorCode = error.message; process.exitCode = evidence.status === "BLOCKED" ? 3 : 1;
} finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "play-product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}

async function verifyPreview(context, client) {
  const platforms = await client.json("GET", "/api/v1/admin/platform-instances?platformId=ps2&limit=100");
  const instance = platforms.items.find(item => item.enabled && item.defaultCoreId === "play");
  assert.ok(instance, "PLAY_PLATFORM_MISSING");
  const uploadId = await client.upload(singleFile(env.RETROM_PLAY_PREVIEW_DISC), "FILES", "GENERAL");
  const imported = await client.json("POST", "/api/v1/admin/imports", {
    headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []},
  });
  const review = await reviewForImport(client, imported.importJobId);
  const preview = await previewCart(client, review.itemId);
  const opened = await openPlay(context, base, preview, evidence);
  await reachPlayFrame(opened, 120); await capturePlay(opened, directory, "review-preview");
  evidence.preview = {itemId: review.itemId, previewId: preview.previewId};
  await opened.page.close();
}

async function verifyCheckpoint(context, client) {
  const original = await launchCart(client, gameId, seedSave);
  const opened = await openPlay(context, base, original, evidence);
  const blank = await namePixels(opened);
  assert.ok(blank < 20, "PLAY_SEED_MUST_BE_EMPTY_TEAM_NAME");
  await pressPlay(opened, 15); // O -> P in the supplied Ridge Racer V seed.
  await pressPlay(opened, 0);
  const confirmed = await namePixels(opened);
  assert.ok(confirmed > blank + 30, "PLAY_CONFIRM_INPUT_MISSING");
  await capturePlay(opened, directory, "confirmed");
  await pressPlay(opened, 3);
  assert.ok(await namePixels(opened) < 20, "PLAY_CANCEL_INPUT_MISSING");
  await pressPlay(opened, 0); // Keep P in the newly captured execution state.
  await pausePlay(opened);
  const paused = await playFrames(opened);
  await opened.page.waitForTimeout(2000);
  assert.equal(await playFrames(opened), paused, "PLAY_PAUSE_FAILED");
  await capturePlay(opened, directory, "saved");
  const saved = await saveCart(opened.page, original.launchId, "play");
  assert.ok(["play-state-v1", "play-state-v1-storage-v1"].includes(saved.checkpointFormat));
  evidence.audio = await fantasyAudioEvidence(opened.page);
  assert.ok(evidence.audio.nonzeroBuffers > 0, "PLAY_AUDIO_MISSING");
  await opened.page.close();
  const restored = await launchCart(client, gameId, saved.saveStateId);
  assert.notEqual(original.launchId, restored.launchId);
  const resumed = await openPlay(context, base, restored, evidence);
  assert.ok(await namePixels(resumed) > 30, "PLAY_EXECUTION_STATE_NOT_RESTORED");
  await capturePlay(resumed, directory, "restored");
  await pressPlay(resumed, 3);
  assert.ok(await namePixels(resumed) < 20, "PLAY_RESTORED_INPUT_MISSING");
  await capturePlay(resumed, directory, "restored-cancel");
  evidence.checkpoint = {gameId, seedSave, saveStateId: saved.saveStateId, format: saved.checkpointFormat,
    originalLaunchId: original.launchId, restoredLaunchId: restored.launchId, pausedFrame: paused};
  await resumed.page.close();
}

async function verifyCache(context, client) {
  const first = await openPlay(context, base, await launchCart(client, gameId), evidence);
  await reachPlayFrame(first, 900); await pausePlay(first);
  const requests = evidence.rangeRequests, bytes = evidence.rangeBytes;
  await capturePlay(first, directory, "first-boot"); await first.page.close();
  const second = await openPlay(context, base, await launchCart(client, gameId), evidence);
  await reachPlayFrame(second, 300); await pausePlay(second);
  evidence.cache = {additionalRequests: evidence.rangeRequests - requests, additionalBytes: evidence.rangeBytes - bytes};
  assert.equal(evidence.cache.additionalRequests, 0, "PLAY_DISC_CACHE_MISS");
  await capturePlay(second, directory, "cached-boot"); await second.page.close();
}
