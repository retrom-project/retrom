import assert from "node:assert/strict";
import {mkdirSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {pathToFileURL} from "node:url";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, launchCart, saveCart} from "./fantasy_product_client.mjs";
import {observeFantasyAudio, fantasyAudioEvidence} from "./fantasy_fixture.mjs";
import {observePSP, openPSP, waitSkyMenu, waitHalfMinuteMenu, pressPSP, skyMenu,
  pausePSP, capturePSP, exitPSP, halfMinuteSelection} from "./ppsspp_product_browser.mjs";
import {observePSPRange, rangeSummary, assertPartialStartup} from "./ppsspp_range_observation.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/ppsspp-range");
const sdk = env.RETROM_ACCEPTANCE_NODE_MODULES;
const {chromium} = await import(sdk ? pathToFileURL(join(sdk, "playwright/index.mjs")).href : "../../web/node_modules/playwright/index.mjs");
const evidence = {schemaVersion: 1, caseId: "ACC-PSP-002", status: "FAIL", errors: [], launches: [], contentRequests: 0, rangeRequests: 0};
mkdirSync(directory, {recursive: true});
let browser, proxy;
const write = () => writeFileSync(join(directory, "ppsspp-range.json"), JSON.stringify(evidence, null, 2));
const stage = name => {console.log(name); evidence.stage = name; write();};
const watchdog = setTimeout(() => {
  evidence.errorCode = "PSP_RANGE_ACCEPTANCE_TIMEOUT"; write();
  void (async () => {await browser?.close(); await proxy?.close(); process.exit(1);})();
}, 900000);
try {
  assert.ok([base, env.RETROM_PSP_SKY_GAME_ID, env.RETROM_PSP_SECOND_GAME_ID, env.RETROM_PSP_REVIEW_ID,
    env.RETROM_PSP_LEGACY_SAVE_ID, env.RETROM_CHROME_EXECUTABLE].every(Boolean), "PSP_RANGE_INPUT_REQUIRED");
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", ...(env.RETROM_ACCEPTANCE_SOFTWARE_GL === "1" ? ["--use-angle=swiftshader", "--enable-unsafe-swiftshader"] : [])]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context); await observeFantasyAudio(context); await observePSP(context, evidence);
  const network = observePSPRange(context), client = await fantasyClient(context, base);
  stage("review-preview");
  const preview = await openPSP(context, base, await previewCart(client, env.RETROM_PSP_REVIEW_ID), evidence);
  await waitSkyMenu(preview); await pausePSP(preview); await network.flush();
  evidence.preview = assertPartialStartup(rangeSummary(network.requests, preview.config.resources.find(item => item.role === "game")));
  await capturePSP(preview, directory, "range-preview"); await preview.page.close();
  // A separate context proves a cold product launch, independent of preview/browser caches.
  await context.close();
  for (const [name, gameId] of [["sky", env.RETROM_PSP_SKY_GAME_ID], ["second", env.RETROM_PSP_SECOND_GAME_ID]]) {
    stage(`cold-${name}`);
    await checkGame(name, gameId);
  }
  assert.ok(evidence.rangeRequests > 0, "PSP_RANGE_MISSING");
  assert.equal(evidence.contentRequests, evidence.rangeRequests, "PSP_WHOLE_DISC_REQUEST");
  assert.deepEqual(evidence.errors, []); evidence.status = "PASS";
} catch (error) {evidence.errorCode = error.message; evidence.stack = error.stack; process.exitCode = 1;}
finally {clearTimeout(watchdog); await browser?.close(); await proxy?.close(); write(); console.log(JSON.stringify(evidence));}

async function checkGame(name, gameId) {
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context); await observeFantasyAudio(context); await observePSP(context, evidence);
  const network = observePSPRange(context), client = await fantasyClient(context, base);
  const launch = await launchCart(client, gameId), first = await openPSP(context, base, launch, evidence);
  if (name === "sky") await waitSkyMenu(first); else await waitHalfMinuteMenu(first);
  await pausePSP(first); await network.flush();
  const source = first.config.resources.find(item => item.role === "game");
  evidence[name] = assertPartialStartup(rangeSummary(network.requests, source)); stage(`menu-${name}`);
  await capturePSP(first, directory, `range-${name}-cold`);
  if (name === "sky") await checkSave(context, client, network, first, launch, gameId);
  else {
    await pressPSP(first, 13); assert.equal(await halfMinuteSelection(first), 1);
    await pressPSP(first, 12); assert.equal(await halfMinuteSelection(first), 0);
    await pressPSP(first, 0); await first.page.waitForTimeout(2500);
    assert.equal(await halfMinuteSelection(first), null); await capturePSP(first, directory, "range-second-confirmed");
    await exitPSP(first, launch.launchId, evidence, `${base}/games/${gameId}`);
  }
  await network.flush(); rangeSummary(network.requests, source);
  await context.close();
}

async function checkSave(context, client, network, first, launch, gameId) {
  await pressPSP(first, 13); const selected = (await skyMenu(first)).selected; assert.equal(selected, 1);
  const saved = await saveCart(first.page, launch.launchId, "ppsspp");
  assert.equal(saved.checkpointFormat, "ppsspp-state-v1-storage-v1");
  evidence.audio = await fantasyAudioEvidence(first.page); assert.ok(evidence.audio.nonzeroBuffers > 0);
  await exitPSP(first, launch.launchId, evidence, `${base}/games/${gameId}`); await network.flush();
  const before = network.requests.length, restoredLaunch = await launchCart(client, gameId, saved.saveStateId);
  const restored = await openPSP(context, base, restoredLaunch, evidence); await pausePSP(restored); await network.flush();
  assert.equal((await skyMenu(restored)).selected, selected);
  evidence.restore = {saveStateId: saved.saveStateId, additionalRequests: network.requests.length - before};
  assert.equal(evidence.restore.additionalRequests, 0, "PSP_BLOCK_CACHE_MISS");
  await capturePSP(restored, directory, "range-restored");
  await pressPSP(restored, 12); assert.equal((await skyMenu(restored)).selected, 0);
  await pressPSP(restored, 0); await restored.page.waitForTimeout(2500);
  assert.notEqual((await skyMenu(restored)).selected, 0);
  await exitPSP(restored, restoredLaunch.launchId, evidence, `${base}/games/${gameId}`);
  stage("legacy-save");
  const legacyLaunch = await launchCart(client, gameId, env.RETROM_PSP_LEGACY_SAVE_ID);
  const legacy = await openPSP(context, base, legacyLaunch, evidence);
  assert.equal((await skyMenu(legacy)).selected, 1, "PSP_LEGACY_STATE_NOT_RESTORED");
  await pressPSP(legacy, 12); assert.equal((await skyMenu(legacy)).selected, 0);
  evidence.legacySave = {saveStateId: env.RETROM_PSP_LEGACY_SAVE_ID, restored: true};
  await capturePSP(legacy, directory, "range-legacy-restored");
  await exitPSP(legacy, legacyLaunch.launchId, evidence, `${base}/games/${gameId}`);
}
