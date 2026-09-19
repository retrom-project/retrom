import {withWasm4RunCart} from "./wasm4_run_cart.mjs";
import {canvasImage} from "./canvas_image.mjs";
import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {createRequire} from "node:module";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {fantasyClient, previewCart, approveCart, launchCart, runtimeCanvas, gamepad, saveCart} from "./fantasy_product_client.mjs";
import {observeContentIO, contentSourceMatcher} from "./content_io_observation.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {finalContentMetrics} from "./content_io_measurement.mjs";
import {validateContentResources} from "./content_io_performance.mjs";
import {exitContentIOPlayer} from "./content_io_player_exit.mjs";
const sharp = createRequire(new URL("../../web/package.json", import.meta.url))("sharp");
const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/wasm4");
mkdirSync(directory, {recursive: true});
const evidence = {schemaVersion: 1, caseId: "ACC-WASM4-001", runId: env.RETROM_CONTENT_IO_RUN_ID ?? null, status: "FAIL", stages: [], errors: []};
let browser, proxy, collector;
try {
  if (![base, env.RETROM_WASM4_CART, env.RETROM_CHROME_EXECUTABLE, env.RETROM_ACCEPTANCE_USERNAME,
    env.RETROM_ACCEPTANCE_PASSWORD].every(Boolean)) {evidence.status = "BLOCKED"; throw Error("WASM4_ACCEPTANCE_INPUT_REQUIRED");}
  const fixture = new URL("../../testdata/public-roms/wasm4-controls/controls.wasm", import.meta.url);
  assert.equal(createHash("sha256").update(readFileSync(fixture)).digest("hex"), "c19447a62cb51bbe9b91e3ef3002c598972bd20bfb85f96668e5c05e85e256cd");
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context);
  collector = await observeContentStoreEvents(context, {retain: true});
  const client = await fantasyClient(context, base);
  let fixtureSize;
  const review = await withWasm4RunCart(fixture, (filename, bytes, receipt) => {
    evidence.ownedSource = receipt;
    fixtureSize = bytes.length;
    evidence.fixtureSha256 = createHash("sha256").update(bytes).digest("hex");
    return importCart(client, filename);
  });
  const preview = await previewCart(client, review.itemId);
  const cold = await open(context, preview);
  evidence.previewPosition = await position(cold.canvas, "preview");
  assert.deepEqual(evidence.previewPosition, {x: 20, y: 20});
  await cold.observer.flush();
  const requests = cold.observer.requests.filter(item => item.method !== "HEAD");
  assert.equal(requests.length, 1, "WASM4_COLD_CART_REQUESTS");
  assert.equal(requests[0].status, 200); assert.equal(requests[0].sizeBytes, fixtureSize);
  evidence.coldCartRequests = requests.length; evidence.coldCartBytes = requests[0].sizeBytes;
  cold.observer.close(); await cold.page.close();
  const {gameId} = await approveCart(client, review.itemId);
  evidence.games = {owned: gameId};
  evidence.stages.push("import-preview-publish");
  const launch = await launchCart(client, gameId), first = await open(context, launch);
  await gamepad(first.page, 15, 160); const moved = await position(first.canvas, "moved");
  assert.ok(moved.x > 20); assert.equal(moved.y, 20);
  await gamepad(first.page, 0, 100); const savedPosition = await position(first.canvas, "saved");
  assert.equal(savedPosition.y, 40);
  const saved = await saveCart(first.page, launch.launchId, "wasm4");
  assert.equal(saved.checkpointFormat, "wasm4-state-v1-storage-v1");
  await exit(first, launch, gameId);
  const restored = await launchCart(client, gameId, saved.saveStateId), resumed = await open(context, restored);
  assert.notEqual(restored.launchId, launch.launchId);
  assert.ok(resumed.config.restore.sizeBytes > 0);
  assert.equal(resumed.config.restore.format, saved.checkpointFormat);
  const restoredPosition = await position(resumed.canvas, "restored");
  assert.deepEqual(restoredPosition, savedPosition);
  await gamepad(resumed.page, 14, 100);
  const restoredInputPosition = await position(resumed.canvas, "restored-input");
  assert.ok(restoredInputPosition.x < savedPosition.x);
  await resumed.observer.flush(); assert.equal(resumed.observer.requests.length, 0, "WASM4_WARM_CART_NETWORK");
  evidence.warmCartRequests = resumed.observer.requests.length;
  await exit(resumed, restored, gameId);
  evidence.checkpoint = {originalLaunchId: launch.launchId, restoredLaunchId: restored.launchId,
    saveStateId: saved.saveStateId, sizeBytes: resumed.config.restore.sizeBytes, savedPosition, restoredPosition, restoredInputPosition};
  evidence.stages.push("direction-confirm-save-different-launch-restore-input-cache");
  const cleanLaunch = await launchCart(client, gameId), clean = await open(context, cleanLaunch);
  assert.deepEqual(await position(clean.canvas, "clean"), {x: 20, y: 20});
  await exit(clean, cleanLaunch, gameId);
  const externalReview = await withWasm4RunCart(env.RETROM_WASM4_CART, (filename, _bytes, receipt) => {
    evidence.externalSource = receipt; return importCart(client, filename);
  });
  const externalPreview = await open(context, await previewCart(client, externalReview.itemId));
  await gamepad(externalPreview.page, 15); await externalPreview.canvas.screenshot({path: join(directory, "external-preview.png")});
  externalPreview.observer.close(); await externalPreview.page.close();
  const externalGame = await approveCart(client, externalReview.itemId);
  evidence.games.external = externalGame.gameId;
  const externalLaunch = await launchCart(client, externalGame.gameId), external = await open(context, externalLaunch);
  await gamepad(external.page, 15); await gamepad(external.page, 0);
  await external.canvas.screenshot({path: join(directory, "external-input.png")});
  await exit(external, externalLaunch, externalGame.gameId);
  evidence.stages.push("clean-restart-external-import-preview-publish-input");
  assert.deepEqual(evidence.errors, []); evidence.status = "PASS";
} catch (error) {evidence.errorCode = error.message; process.exitCode = evidence.status === "BLOCKED" ? 3 : 1;}
finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "wasm4-product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}
if (env.RETROM_CONTENT_IO_RUN_ID && evidence.status === "PASS") {
  try {
    const {completeWasm4ContentProof} = await import("./wasm4_content_proof.mjs");
    await completeWasm4ContentProof(directory, evidence);
  } catch (error) {console.error(error.message); process.exitCode = 1;}
}
async function exit(opened, launch, gameId) {
  await exitContentIOPlayer(opened.page, base, {...launch, returnTo: `/games/${gameId}`});
  const sessions = collector.snapshot(opened.page), metrics = finalContentMetrics(sessions);
  validateContentResources(metrics.publicPeak, false); validateContentResources(metrics.closed, true);
  (evidence.contentIO ??= []).push({launchId: launch.launchId, sessions});
  opened.observer.close(); await opened.page.close();
}
async function importCart(client, filename) {
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {headers: client.writeHeaders(), data: {}});
  const instances = await client.json("GET", "/api/v1/admin/platform-instances?platformId=wasm4&limit=100");
  const instance = instances.items.find(item => item.enabled && item.defaultCoreId === "wasm4"); assert.ok(instance);
  const uploadId = await client.upload(singleFile(filename), "FILES", "GENERAL");
  const imported = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []}});
  return reviewForImport(client, imported.importJobId);
}
async function open(context, launch) {
  const response = await context.request.get(`${base}/runtime/launches/${launch.launchId ?? launch.previewId}/config`);
  assert.equal(response.status(), 200); const config = await response.json();
  assert.equal(config.runtime.targetId, "wasm4");
  (evidence.runtimes ??= []).push({providerVersion: config.runtime.providerVersion,
    bundleSha256: config.runtime.bundleSha256, moduleSha256: config.runtime.moduleSha256});
  const sources = config.resources.filter(item => item.role === "game"); assert.equal(sources.length, 1);
  const observer = observeContentIO(context, contentSourceMatcher(sources, base));
  const page = await context.newPage(); page.on("pageerror", error => evidence.errors.push(error.message));
  await page.goto(`${base}${launch.playUrl}`, {waitUntil: "domcontentloaded"});
  const canvas = await runtimeCanvas(page, "WASM-4");
  await page.getByRole("status").filter({hasText: "可创建存档"}).waitFor({state: "attached", timeout: 60000});
  return {page, canvas, observer, config};
}
async function position(canvas, name) {
  const png = await canvasImage(canvas);
  writeFileSync(join(directory, `${name}.png`), png);
  const pixels = await sharp(png).resize(160, 160, {kernel: "nearest"}).removeAlpha().raw().toBuffer();
  let x = 160, y = 160, count = 0;
  for (let row = 0; row < 160; row++) for (let column = 0; column < 160; column++) {
    const i = (row * 160 + column) * 3;
    if ([0, 1, 2].some(channel => Math.abs(pixels[i + channel] - pixels[channel]) > 20)) {
      x = Math.min(x, column); y = Math.min(y, row); count++;
    }
  }
  assert.ok(count >= 80 && count <= 121, "WASM4_FIXTURE_SQUARE_MISSING");
  return {x, y};
}
