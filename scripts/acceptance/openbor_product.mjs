import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {mkdir, readFile, writeFile} from "node:fs/promises";
import {gunzipSync} from "node:zlib";
import {createProductClient, singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {observeOpenBOR, nativeState, assertRobo, spriteX} from "./openbor_observation.mjs";
import {advanceToNativeSave, enterRobo, pad, saveAndExit, checkPause} from "./openbor_actions.mjs";
import {observeOpenBORAudio, openborAudio} from "./openbor_audio.mjs";
import {revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";
const {chromium} = await import(process.env.RETROM_PLAYWRIGHT_MODULE ?? "../../web/node_modules/playwright/index.mjs");
const base = process.env.RETROM_ACCEPTANCE_BASE_URL, gameFile = process.env.RETROM_OPENBOR_GAME;
const output = process.env.RETROM_ACCEPTANCE_CASE_DIR;
assert(base && gameFile && output, "OPENBOR_MANUAL_INPUT_REQUIRED");
await mkdir(output, {recursive: true});
const evidence = {schemaVersion: 1, caseId: "ACC-OPENBOR-001", status: "FAIL", errors: [], stages: []};
const capabilities = {secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true};
const sha = bytes => createHash("sha256").update(bytes).digest("hex");
let browser;
try {
  evidence.gameSha256 = sha(await readFile(gameFile));
  browser = await chromium.launch({headless: true, executablePath: process.env.RETROM_CHROME_EXECUTABLE,
    args: ["--host-resolver-rules=MAP *.localhost 127.0.0.1", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  evidence.browser = browser.version();
  const context = await browser.newContext({viewport: {width: 1440, height: 1000}});
  await installVirtualStandardGamepad(context); await observeOpenBOR(context); await observeOpenBORAudio(context);
  context.setDefaultTimeout(15000);
  const login = await context.request.post(`${base}/api/v1/auth/login`, {headers: {Origin: base},
    data: {username: process.env.RETROM_ACCEPTANCE_USERNAME, password: process.env.RETROM_ACCEPTANCE_PASSWORD}});
  assert.equal(login.status(), 200);
  const client = createProductClient(context, base, (await login.json()).csrfToken);
  const review = process.env.RETROM_OPENBOR_REVIEW_ID
    ? await client.json("GET", `/api/v1/admin/reviews/${process.env.RETROM_OPENBOR_REVIEW_ID}`) : await importGame(client);
  evidence.reviewId = review.itemId;
  const preview = await client.json("POST", `/api/v1/admin/reviews/${review.itemId}/previews`, {headers: client.writeHeaders(), expected: 201,
    data: {clientCapabilities: capabilities}});
  const trial = await open(context, preview, "preview");
  await enterRobo(trial.page, trial.canvas); const trialState = await nativeState(trial.page);
  assert.equal(trialState.levels[0], "data/levels/l1s1.txt");
  await pad(trial.page, 15, 300, 100);
  await trial.canvas.screenshot({path: `${output}/preview-gameplay.png`}); await trial.page.close();
  evidence.stages.push(process.env.RETROM_OPENBOR_REVIEW_ID ? "existing-review-preview" : "import-review-preview");
  const snapshot = await client.raw("GET", `/api/v1/admin/reviews/${review.itemId}`);
  const approved = process.env.RETROM_OPENBOR_GAME_ID ? {gameId: process.env.RETROM_OPENBOR_GAME_ID} : await client.json("POST", `/api/v1/admin/reviews/${review.itemId}/approve`, {
    headers: {...client.writeHeaders(), "If-Match": snapshot.headers().etag}, expected: 201, data: {}});
  evidence.gameId = approved.gameId; evidence.seeded = Boolean(process.env.RETROM_OPENBOR_GAME_ID);
  const original = await launch(client, approved.gameId);
  const playing = await open(context, original, "product");
  await enterRobo(playing.page, playing.canvas);
  const before = await spriteX(playing.canvas); await pad(playing.page, 15, 500, 100);
  const moved = await spriteX(playing.canvas);
  console.log("movement", before, moved);
  assert(before.count > 20 && moved.count > 20 && Math.abs(moved.x - before.x) > 5, "OPENBOR_GAMEPAD_MOVEMENT_MISSING");
  await pad(playing.page, 0, 150, 100); await playing.canvas.press("ArrowLeft", {delay: 180});
  await playing.canvas.screenshot({path: `${output}/product-input.png`});
  evidence.audio = await openborAudio(playing.page);
  assert(evidence.audio.nonzeroBuffers > 0, "OPENBOR_AUDIO_SIGNAL_MISSING");
  evidence.pause = await checkPause(playing.page, playing.canvas, output);
  const originalState = await advanceToNativeSave(playing.page, playing.canvas, output); assertRobo(originalState);
  evidence.original = {launchId: original.launchId, state: originalState};
  await revealPreviewToolbar(playing.page);
  console.log("toolbar", (await playing.page.locator("body").innerText()).slice(0, 1200));
  const saved = await saveAndExit(playing.page, original.launchId);
  assert.equal(saved.receipt.checkpointFormat, "openbor-game-save-v1-storage-v1");
  const nativeBytes = gunzipSync(saved.bytes, {maxOutputLength: 16 * 1024 * 1024});
  const decoded = JSON.parse(nativeBytes.toString("utf8")); assert.equal(decoded.identity, evidence.gameSha256);
  assert(saved.bytes.length < nativeBytes.length, "OPENBOR_STORAGE_NOT_COMPRESSED");
  evidence.save = {...saved.receipt, sizeBytes: saved.bytes.length, nativeBytes: nativeBytes.length, sha256: sha(saved.bytes)};
  await playing.page.close();
  const restored = await launch(client, approved.gameId, saved.receipt.saveStateId);
  assert.notEqual(restored.launchId, original.launchId);
  const resumed = await open(context, restored, "restore");
  const initial = await nativeState(resumed.page);
  assert.deepEqual(initial.initialSave.progress, originalState.progress, "OPENBOR_RESTORE_FILES_MISMATCH");
  await enterRobo(resumed.page, resumed.canvas, true);
  const restoredState = await nativeState(resumed.page); assertRobo(restoredState);
  assert.equal(restoredState.levels[0], originalState.levels.at(-1), "OPENBOR_RESTORE_LEVEL_MISMATCH");
  await resumed.canvas.screenshot({path: `${output}/restored-gameplay.png`});
  const restoredX = await spriteX(resumed.canvas); await pad(resumed.page, 15, 500, 100);
  assert(Math.abs((await spriteX(resumed.canvas)).x - restoredX.x) > 5, "OPENBOR_RESTORED_INPUT_MISSING");
  await resumed.canvas.screenshot({path: `${output}/restored-input.png`}); await resumed.page.close();
  evidence.restored = {launchId: restored.launchId, state: restoredState};
  const clean = await open(context, await launch(client, approved.gameId), "clean");
  assert.equal((await nativeState(clean.page)).initialSave.saveBytes, 0, "OPENBOR_UNREQUESTED_RESTORE");
  await clean.page.close();
  evidence.stages.push(`${evidence.seeded ? "existing-game" : "publish"}-launch-input-native-save-new-launch-load-input-clean-restart`);
  assert.equal(evidence.errors.length, 0, "OPENBOR_BROWSER_ERRORS"); evidence.status = "PASS";
} catch (error) {evidence.errorCode = error.message; process.exitCode = 1;}
finally {
  await browser?.close(); await writeFile(`${output}/openbor-product.json`, JSON.stringify(evidence, null, 2));
  console.log(JSON.stringify(evidence));
}
async function importGame(client) {
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {headers: client.writeHeaders(), data: {}});
  const instances = await client.json("GET", "/api/v1/admin/platform-instances?platformId=openbor&limit=100");
  const instance = instances.items.find(item => item.enabled && item.defaultCoreId === "openbor"); assert(instance);
  const uploadId = await client.upload(singleFile(gameFile), "FILES", "GENERAL");
  const imported = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []}});
  return reviewForImport(client, imported.importJobId);
}
async function launch(client, gameId, saveStateId = null) {
  return client.json("POST", "/api/v1/launches", {headers: client.writeHeaders(), expected: 201,
    data: {gameId, coreId: null, saveStateId, dosEntry: null, returnTo: `/games/${gameId}`, clientCapabilities: capabilities}});
}
async function open(context, launch, label) {
  const page = await context.newPage();
  page.on("pageerror", error => {evidence.errors.push(error.message); console.log(error.stack);});
  page.on("console", message => {if (message.type() === "warning" || message.type() === "error") console.log(message.text().slice(-1000));});
  const configTask = page.waitForResponse(response => /^\/runtime\/launches\/[^/]+\/config$/u.test(new URL(response.url()).pathname), {timeout: 60000});
  await page.goto(`${base}${launch.playUrl}`, {waitUntil: "domcontentloaded", timeout: 60000});
  const envelope = await (await configTask).json();
  evidence.providers ??= []; evidence.providers.push({label, providerId: envelope.runtime.providerId, targetId: envelope.runtime.targetId, bundleSha256: envelope.runtime.bundleSha256, moduleSha256: envelope.runtime.moduleSha256});
  if (label === "restore") {
    assert.equal(envelope.restore.sha256, evidence.save.sha256);
    const payload = await context.request.get(new URL(envelope.restore.url, base).href);
    assert.equal(payload.status(), 200); assert.equal(sha(await payload.body()), evidence.save.sha256);
  }
  for (let i = 0; i < 600; i++) {
    const state = await nativeState(page);
    if (state?.frames >= 2) break;
    await page.waitForTimeout(100);
  }
  await page.waitForTimeout(15000);
  const resume = page.getByRole("button", {name: "继续游戏", exact: true}); if (await resume.isVisible()) await resume.click();
  for (const frame of page.frames()) {
    const canvas = frame.locator('canvas[aria-label="openbor game"]');
    if (await canvas.isVisible()) {await canvas.click(); await canvas.screenshot({path: `${output}/${label}.png`}); return {page, canvas};}
  }
  throw Error("OPENBOR_CANVAS_MISSING");
}
