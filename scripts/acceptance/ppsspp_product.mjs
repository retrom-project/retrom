import assert from "node:assert/strict";
import {mkdirSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {gunzipSync} from "node:zlib";
import {createHash} from "node:crypto";
import {pathToFileURL} from "node:url";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, launchCart, saveCart} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {observeFantasyAudio, fantasyAudioEvidence} from "./fantasy_fixture.mjs";
import {observePSPRange, rangeSummary, assertPartialStartup} from "./ppsspp_range_observation.mjs";
import {observePSP, openPSP, capturePSP, pressPSP, pausePSP, pspFrames, skyMenu, waitSkyMenu,
  waitHalfMinuteMenu, halfMinuteSelection, exitPSP, checkPSPLayout} from "./ppsspp_product_browser.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/ppsspp-product");
const sdk = env.RETROM_ACCEPTANCE_NODE_MODULES;
const {chromium} = await import(sdk ? pathToFileURL(join(sdk, "playwright/index.mjs")).href : "../../web/node_modules/playwright/index.mjs");
const evidence = {schemaVersion: 1, caseId: "ACC-PSP-001", status: "FAIL", errors: [], launches: [], contentRequests: 0, rangeRequests: 0};
mkdirSync(directory, {recursive: true});
const progressPath = join(directory, "progress.json"), progress = {};
const stage = name => {console.log(name); writeFileSync(progressPath, JSON.stringify(progress, null, 2));};
let browser, proxy;
const watchdog = setTimeout(() => {
  evidence.errorCode = "PSP_ACCEPTANCE_TIMEOUT";
  writeFileSync(join(directory, "ppsspp-product.json"), JSON.stringify(evidence, null, 2));
  void (async () => {await browser?.close(); await proxy?.close(); process.exit(1);})();
}, 900000);
try {
  assert.ok([base, env.RETROM_PSP_SKY_DISC, env.RETROM_PSP_SECOND_DISC, env.RETROM_CHROME_EXECUTABLE,
    env.RETROM_ACCEPTANCE_USERNAME, env.RETROM_ACCEPTANCE_PASSWORD].every(Boolean), "PSP_ACCEPTANCE_INPUT_REQUIRED");
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", ...(env.RETROM_ACCEPTANCE_SOFTWARE_GL === "1" ? ["--use-angle=swiftshader", "--enable-unsafe-swiftshader"] : [])]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context); await observeFantasyAudio(context); await observePSP(context, evidence);
  const network = observePSPRange(context);
  const client = await fantasyClient(context, base);
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {headers: client.writeHeaders(), data: {}});
  const platforms = await client.json("GET", "/api/v1/admin/platform-instances?platformId=psp&limit=100");
  const instance = platforms.items.find(item => item.enabled && item.defaultCoreId === "ppsspp");
  assert.ok(instance, "PSP_PLATFORM_MISSING");
  for (const [key, path] of [["sky", env.RETROM_PSP_SKY_DISC], ["second", env.RETROM_PSP_SECOND_DISC]]) {
    if (!progress[key]) {
      stage(`upload-${key}`);
      const uploadId = await client.upload(singleFile(path), "FILES", "GENERAL");
      const imported = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
        data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []}});
      stage(`wait-review-${key}:${imported.importJobId}`);
      progress[key] = {review: await reviewForImport(client, imported.importJobId, {attempts: 600, waitMs: 200})}; stage(`import-${key}`);
    }
    const preview = await openPSP(context, base, await previewCart(client, progress[key].review.itemId), evidence);
    if (key === "sky") await waitSkyMenu(preview); else await waitHalfMinuteMenu(preview);
    await pausePSP(preview); await network.flush();
    (evidence.coldStarts ??= {})[key] = assertPartialStartup(rangeSummary(network.requests, preview.config.resources.find(item => item.role === "game")));
    await capturePSP(preview, directory, `review-${key}`); await preview.page.close(); stage(`preview-${key}`);
    const snapshot = await client.raw("GET", `/api/v1/admin/reviews/${progress[key].review.itemId}`);
    assert.equal(snapshot.status(), 200);
    const duplicates = (await snapshot.json()).duplicateGames;
    const approved = await client.json("POST", `/api/v1/admin/reviews/${progress[key].review.itemId}/approve`, {
      headers: {...client.writeHeaders(), "If-Match": snapshot.headers().etag}, expected: 201,
      data: duplicates.length ? {duplicatePolicy: "ALLOW_NEW", acknowledgedGameIds: duplicates.map(game => game.gameId)} : {},
    });
    progress[key].gameId = approved.gameId; stage(`publish-${key}`);
  }
  const firstLaunch = await launchCart(client, progress.sky.gameId);
  const first = await openPSP(context, base, firstLaunch, evidence);
  const initial = await waitSkyMenu(first), direction = initial.selected === 4 ? 12 : 13;
  evidence.layout = [];
  for (const size of [{width: 900, height: 700}, {width: 1280, height: 900}]) {
    await first.page.setViewportSize(size); await first.page.waitForTimeout(300);
    evidence.layout.push(await checkPSPLayout(first));
    await first.page.screenshot({path: join(directory, `player-${size.width}.png`)});
  }
  await pressPSP(first, direction);
  const selected = await skyMenu(first);
  assert.equal(selected.selected, initial.selected + (direction === 12 ? -1 : 1), "PSP_DIRECTION_MISSING");
  assert.notEqual(selected.selected, null, "PSP_MENU_MISSING"); await capturePSP(first, directory, "selected");
  await pausePSP(first); const paused = await pspFrames(first); await first.page.waitForTimeout(1500);
  assert.equal(await pspFrames(first), paused, "PSP_PAUSE_FAILED");
  const saved = await saveCart(first.page, firstLaunch.launchId, "ppsspp");
  assert.equal(saved.checkpointFormat, "ppsspp-state-v1-storage-v1");
  evidence.audio = await fantasyAudioEvidence(first.page); assert.ok(evidence.audio.nonzeroBuffers > 0, "PSP_AUDIO_MISSING");
  evidence.checkpoint = {saveStateId: saved.saveStateId, format: saved.checkpointFormat, selected, firstLaunchId: firstLaunch.launchId};
  await exitPSP(first, firstLaunch.launchId, evidence, base + `/games/${progress.sky.gameId}`); stage("saved");
  const requests = evidence.contentRequests, restoredLaunch = await launchCart(client, progress.sky.gameId, saved.saveStateId);
  assert.notEqual(restoredLaunch.launchId, firstLaunch.launchId);
  const restored = await openPSP(context, base, restoredLaunch, evidence);
  await pausePSP(restored); await network.flush();
  evidence.cache = {additionalRequests: evidence.contentRequests - requests}; assert.equal(evidence.cache.additionalRequests, 0, "PSP_CACHE_MISS");
  const response = await client.raw("GET", restored.config.restore.url);
  assert.equal(response.status(), 200); const stored = await response.body();
  assert.equal(stored.length, restored.config.restore.sizeBytes);
  assert.equal(createHash("sha256").update(stored).digest("hex"), restored.config.restore.sha256);
  const native = gunzipSync(stored, {maxOutputLength: 268435456});
  const headerSize = native.readUInt32LE(0), header = JSON.parse(native.subarray(4, 4 + headerSize).toString("utf8"));
  assert.equal(header.version, 1); assert.ok(header.stateSize > 0); assert.ok(Array.isArray(header.files));
  assert.equal(4 + headerSize + header.stateSize + header.files.reduce((n, file) => n + file.size, 0), native.length);
  evidence.checkpoint.storedBytes = stored.length; evidence.checkpoint.nativeBytes = native.length;
  assert.equal((await skyMenu(restored)).selected, selected.selected, "PSP_EXECUTION_STATE_NOT_RESTORED");
  await capturePSP(restored, directory, "restored");
  await pressPSP(restored, direction === 12 ? 13 : 12);
  assert.equal((await skyMenu(restored)).selected, initial.selected, "PSP_RESTORED_INPUT_MISSING");
  await pressPSP(restored, 0); await restored.page.waitForTimeout(2500); await capturePSP(restored, directory, "confirmed");
  assert.notDeepEqual(await skyMenu(restored), initial, "PSP_CONFIRM_MISSING");
  rangeSummary(network.requests, restored.config.resources.find(item => item.role === "game"));
  assert.ok(evidence.rangeRequests > 0, "PSP_RANGE_MISSING");
  assert.equal(evidence.rangeRequests, evidence.contentRequests, "PSP_WHOLE_DISC_REQUEST");
  evidence.checkpoint.restoredLaunchId = restoredLaunch.launchId;
  await exitPSP(restored, restoredLaunch.launchId, evidence, base + `/games/${progress.sky.gameId}`); stage("restored-input-confirm");
  const secondLaunch = await launchCart(client, progress.second.gameId);
  const second = await openPSP(context, base, secondLaunch, evidence);
  await waitHalfMinuteMenu(second); await pressPSP(second, 13);
  assert.equal(await halfMinuteSelection(second), 1, "PSP_SECOND_DIRECTION_MISSING");
  await pressPSP(second, 12); assert.equal(await halfMinuteSelection(second), 0);
  await capturePSP(second, directory, "second-menu"); await pressPSP(second, 0);
  await second.page.waitForTimeout(2500); await capturePSP(second, directory, "second-confirmed");
  assert.equal(await halfMinuteSelection(second), null, "PSP_SECOND_CONFIRM_MISSING");
  await exitPSP(second, secondLaunch.launchId, evidence, base + `/games/${progress.second.gameId}`);
  assert.deepEqual(evidence.errors, []); evidence.status = "PASS";
} catch (error) {evidence.errorCode = error.message; evidence.stack = error.stack; process.exitCode = 1;}
finally {
  clearTimeout(watchdog); await browser?.close(); await proxy?.close(); evidence.games = progress;
  writeFileSync(join(directory, "ppsspp-product.json"), JSON.stringify(evidence, null, 2) + "\n"); console.log(JSON.stringify(evidence));
}
