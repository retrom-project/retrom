import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {mkdir, readFile, writeFile} from "node:fs/promises";
import {gunzipSync} from "node:zlib";
import {createProductClient, singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {captureOptionalReviewScreenshot} from "./rpgmaker_preview_actions.mjs";
import {observeNXEngine, nativeState, pad} from "./nxengine_observation.mjs";
import {enterGame, sprite, checkPause, saveAtStartPoint, saveAndExit} from "./nxengine_actions.mjs";

const base = process.env.RETROM_ACCEPTANCE_BASE_URL, gameFile = process.env.RETROM_NXENGINE_GAME;
const output = process.env.RETROM_ACCEPTANCE_CASE_DIR;
assert(output, "NXENGINE_OUTPUT_REQUIRED");
await mkdir(output, {recursive: true});
if (!base || !gameFile || !process.env.RETROM_ACCEPTANCE_USERNAME || !process.env.RETROM_ACCEPTANCE_PASSWORD) {
  await writeFile(`${output}/nxengine-product.json`, JSON.stringify({schemaVersion: 1, caseId: "ACC-NXENGINE-001",
    status: "BLOCKED", reason: "NXENGINE_MANUAL_INPUT_REQUIRED"}));
  process.exit(3);
}
const {chromium} = await import(process.env.RETROM_PLAYWRIGHT_MODULE ?? "../../web/node_modules/playwright/index.mjs");
const evidence = {schemaVersion: 1, caseId: "ACC-NXENGINE-001", status: "FAIL", errors: [], stages: [], providers: []};
const capabilities = {secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true};
const sha = bytes => createHash("sha256").update(bytes).digest("hex");
let browser, proxy, activePage, restoredBytes;
try {
  evidence.gameArchiveSha256 = sha(await readFile(gameFile));
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({headless: true, executablePath: process.env.RETROM_CHROME_EXECUTABLE,
    env: Object.fromEntries(Object.entries(process.env).filter(([key]) => !["DISPLAY", "WAYLAND_DISPLAY"].includes(key))),
    args: ["--host-resolver-rules=MAP *.localhost 127.0.0.1"]});
  evidence.browser = browser.version();
  const context = await browser.newContext({viewport: {width: 1280, height: 960}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context); await observeNXEngine(context); context.setDefaultTimeout(15000);
  const login = await context.request.post(`${base}/api/v1/auth/login`, {headers: {Origin: base},
    data: {username: process.env.RETROM_ACCEPTANCE_USERNAME, password: process.env.RETROM_ACCEPTANCE_PASSWORD}});
  assert.equal(login.status(), 200);
  const client = createProductClient(context, base, (await login.json()).csrfToken);
  const review = await importGame(client); evidence.reviewId = review.itemId;
  const preview = await client.json("POST", `/api/v1/admin/reviews/${review.itemId}/previews`, {
    headers: client.writeHeaders(), expected: 201, data: {clientCapabilities: capabilities}});
  const trial = await open(context, preview, "preview");
  await enterGame(trial.page, trial.canvas);
  await trial.canvas.screenshot({path: `${output}/preview-gameplay.png`});
  await captureOptionalReviewScreenshot(trial.page, preview.previewId);
  await trial.page.close(); evidence.stages.push("upload-import-review-preview-screenshot");
  const snapshot = await client.raw("GET", `/api/v1/admin/reviews/${review.itemId}`);
  const approved = await client.json("POST", `/api/v1/admin/reviews/${review.itemId}/approve`, {
    headers: {...client.writeHeaders(), "If-Match": snapshot.headers().etag}, expected: 201, data: {}});
  evidence.gameId = approved.gameId;
  const gameId = approved.gameId;
  const original = await launch(client, gameId), playing = await open(context, original, "product");
  await enterGame(playing.page, playing.canvas);
  const before = await sprite(playing.canvas); await pad(playing.page, 15, 170, 300);
  const after = await sprite(playing.canvas);
  assert(before.count >= 8 && after.count >= 8 && after.x - before.x > 3, "NXENGINE_GAMEPAD_MOVEMENT_MISSING");
  await pad(playing.page, 14, 170, 500);
  await playing.canvas.press("ArrowLeft", {delay: 100}); await playing.page.waitForTimeout(300);
  assert((await sprite(playing.canvas)).x < before.x - 2, "NXENGINE_KEYBOARD_MOVEMENT_MISSING");
  await playing.canvas.press("ArrowRight", {delay: 100}); await playing.page.waitForTimeout(500);
  await playing.canvas.screenshot({path: `${output}/product-input.png`});
  evidence.input = {before, after}; evidence.pause = await checkPause(playing.page, playing.canvas, output);
  const native = await saveAtStartPoint(playing.page, playing.canvas, output);
  assert(native.nonzeroAudio > 0, "NXENGINE_AUDIO_MISSING"); evidence.audioFrames = native.nonzeroAudio;
  const saved = await saveAndExit(playing.page, original.launchId);
  assert.equal(saved.receipt.checkpointFormat, "nxengine-game-save-v1-storage-v1");
  evidence.save = saved.receipt;
  await playing.page.close(); evidence.stages.push("publish-launch-input-pause-native-save-export");
  const restored = await launch(client, gameId, saved.receipt.saveStateId);
  assert.notEqual(restored.launchId, original.launchId);
  const resumed = await open(context, restored, "restore");
  const unpacked = gunzipSync(restoredBytes, {maxOutputLength: 16384});
  const decoded = JSON.parse(unpacked.toString("utf8"));
  assert.equal(decoded.identity, evidence.contentDigest);
  const files = decoded.files.map(([name, data]) => ({name, bytes: [...Buffer.from(data, "base64")]}));
  assert.deepEqual(files, native.saves, "NXENGINE_NATIVE_TRANSFER_MISMATCH");
  assert(restoredBytes.length < unpacked.length, "NXENGINE_STORAGE_NOT_COMPRESSED");
  evidence.save = {...saved.receipt, bytes: restoredBytes.length, nativeBytes: unpacked.length, sha256: sha(restoredBytes),
    profiles: files.map(file => profileSummary(file))};
  assert.deepEqual((await nativeState(resumed.page)).initial.saves, files, "NXENGINE_RESTORE_FILES_MISMATCH");
  await enterGame(resumed.page, resumed.canvas);
  const loaded = await sprite(resumed.canvas);
  assert(Math.abs(loaded.x - evidence.save.profiles[0].x) < 16, "NXENGINE_NATIVE_POSITION_NOT_RESTORED");
  await resumed.canvas.screenshot({path: `${output}/restored-gameplay.png`});
  await pad(resumed.page, 14, 170, 300);
  assert(loaded.x - (await sprite(resumed.canvas)).x > 3, "NXENGINE_RESTORED_INPUT_MISSING");
  await resumed.canvas.screenshot({path: `${output}/restored-input.png`}); await resumed.page.close();
  evidence.launches = {original: original.launchId, restored: restored.launchId};
  const clean = await open(context, await launch(client, gameId), "clean");
  assert.deepEqual((await nativeState(clean.page)).initial.saves, [], "NXENGINE_UNREQUESTED_RESTORE");
  await clean.page.close(); evidence.stages.push("new-launch-native-load-input-clean-launch");
  assert.equal(evidence.errors.length, 0, "NXENGINE_BROWSER_ERRORS"); evidence.status = "PASS";
} catch (error) {
  evidence.errorCode = error.message; process.exitCode = 1;
  await activePage?.screenshot({path: `${output}/failure.png`, timeout: 5000}).catch(() => undefined);
} finally {
  await browser?.close(); await proxy?.close();
  await writeFile(`${output}/nxengine-product.json`, JSON.stringify(evidence, null, 2)); console.log(JSON.stringify(evidence));
}
async function importGame(client) {
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {headers: client.writeHeaders(), data: {}});
  const instances = await client.json("GET", "/api/v1/admin/platform-instances?platformId=cavestory&limit=100");
  const instance = instances.items.find(item => item.enabled && item.defaultCoreId === "nxengine"); assert(instance);
  const uploadId = await client.upload(singleFile(gameFile), "FILES", "PROJECT");
  const imported = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "NXENGINE_PROJECT", tagIds: []}});
  return reviewForImport(client, imported.importJobId);
}
async function launch(client, gameId, saveStateId = null) {
  return client.json("POST", "/api/v1/launches", {headers: client.writeHeaders(), expected: 201,
    data: {gameId, coreId: null, saveStateId, dosEntry: null, returnTo: `/games/${gameId}`, clientCapabilities: capabilities}});
}
async function open(context, launch, label) {
  const page = await context.newPage(); activePage = page;
  page.on("pageerror", error => evidence.errors.push(error.message));
  const config = page.waitForResponse(response => /^\/runtime\/launches\/[^/]+\/config$/u.test(new URL(response.url()).pathname), {timeout: 60000});
  await page.goto(`${base}${launch.playUrl}`, {waitUntil: "domcontentloaded", timeout: 60000});
  const envelope = await (await config).json();
  assert.equal(envelope.runtime.targetId, "nxengine");
  assert.equal(envelope.runtime.checkpoint.semantics, "GAME_SAVE");
  evidence.contentDigest = envelope.resources.find(resource => resource.role === "game").contentDigest;
  evidence.providers.push({label, providerId: envelope.runtime.providerId, targetId: envelope.runtime.targetId,
    bundleSha256: envelope.runtime.bundleSha256, moduleSha256: envelope.runtime.moduleSha256});
  if (label === "restore") {
    const response = await context.request.get(new URL(envelope.restore.url, base).href);
    assert.equal(response.status(), 200); restoredBytes = await response.body(); assert.equal(sha(restoredBytes), envelope.restore.sha256);
  }
  for (let i = 0; i < 600; i++) {
    if ((await nativeState(page))?.frames > 2) {
      for (const frame of page.frames()) {
        const canvas = frame.locator('canvas[aria-label="nxengine game"]');
        if (await canvas.isVisible()) {await canvas.click(); return {page, canvas};}
      }
    }
    await page.waitForTimeout(100);
  }
  throw Error("NXENGINE_READY_TIMEOUT");
}
function profileSummary(file) {
  const bytes = Buffer.from(file.bytes); assert.equal(bytes.length, 0x604);
  assert.equal(bytes.subarray(0, 8).toString(), "Do041220");
  return {name: file.name, sha256: sha(bytes), stage: bytes.readUInt32LE(8),
    x: bytes.readInt32LE(16) / 512, y: bytes.readInt32LE(20) / 512};
}
