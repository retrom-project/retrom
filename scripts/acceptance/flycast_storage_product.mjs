import assert from "node:assert/strict";
import {mkdirSync, writeFileSync, readFileSync, existsSync} from "node:fs";
import {join, resolve} from "node:path";
import {createHash} from "node:crypto";
import {gunzipSync} from "node:zlib";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, approveCart, launchCart, gamepad} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {waitForPreviewReady, revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";
const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/flycast-storage");
mkdirSync(directory, {recursive: true});
const evidence = {caseId: "ACC-FLYCAST-001", status: "FAIL", stages: [], errors: [], runtimes: []};
const hash = bytes => createHash("sha256").update(bytes).digest("hex");
let browser, proxy;
async function prepare(client) {
  const file = join(directory, "product-input.json");
  const progress = existsSync(file) ? JSON.parse(readFileSync(file)) : {};
  const digest = hash(readFileSync(env.RETROM_FLYCAST_CHD));
  if (progress.digest) {assert.equal(progress.digest, digest);}
  const requirements = (await client.json("GET", "/api/v1/admin/bios?scope=FULL_CATALOG&coreId=flycast&limit=100")).items;
  for (const item of requirements.filter(item => item.coreId === "flycast")) {
    if (item.status === "MATCHED") {continue;}
    assert.equal(item.activeInstallation, null);
    const uploadId = await client.upload(singleFile(join(env.RETROM_FLYCAST_BIOS_DIR, item.logicalName)), "FILES", "GENERAL");
    const upload = await client.json("GET", `/api/v1/admin/uploads/${uploadId}`);
    await client.json("POST", `/api/v1/admin/bios/${item.id}/installations`, {expected: 201,
      headers: {...client.writeHeaders(), "If-Match": `"v${item.version}"`}, data: {uploadFileId: upload.files[0].fileId}});
  }
  if (!progress.review) {
    await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {headers: client.writeHeaders(), data: {}, expected: 200});
    const platforms = await client.json("GET", "/api/v1/admin/platform-instances?platformId=dreamcast&limit=100");
    const platform = platforms.items.find(item => item.enabled && item.defaultCoreId === "flycast");
    assert.ok(platform, "FLYCAST_PLATFORM_MISSING");
    const uploadId = await client.upload(singleFile(env.RETROM_FLYCAST_CHD), "FILES", "GENERAL");
    const imported = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
      data: {uploadId, targetPlatformInstanceId: platform.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []}});
    progress.review = await reviewForImport(client, imported.importJobId);
    progress.digest = digest; writeFileSync(file, JSON.stringify(progress));
  }
  if (!progress.gameId) {
    const preview = await open(await previewCart(client, progress.review.itemId));
    await preview.page.waitForTimeout(10000); await preview.canvas.screenshot({path: join(directory, "preview.png")});
    await preview.page.close(); evidence.stages.push("import-review-preview");
    progress.gameId = (await approveCart(client, progress.review.itemId)).gameId;
    writeFileSync(file, JSON.stringify(progress));
  } else {evidence.stages.push("reuse-previously-published-game");}
  return progress;
}
async function open(launch) {
  const page = await browser.contexts()[0].newPage();
  page.on("pageerror", error => evidence.errors.push(error.message.slice(0, 200)));
  await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded"});
  await waitForPreviewReady(page);
  let frame;
  for (const candidate of page.frames()) {
    if (await candidate.evaluate(() => Boolean(window.EJS_emulator?.gameManager))) {frame = candidate; break;}
  }
  assert.ok(frame, "FLYCAST_NATIVE_INSTANCE_MISSING");
  const config = await page.evaluate(async id => (await fetch(`/runtime/launches/${id}/config`)).json(), launch.launchId);
  assert.equal(config.runtime.targetId, "flycast");
  evidence.runtimes.push({providerVersion: config.runtime.providerVersion, bundleSha256: config.runtime.bundleSha256,
    moduleSha256: config.runtime.moduleSha256});
  return {page, frame, config, canvas: frame.locator("canvas").first()};
}
async function save(opened, client, gameId, launchId) {
  const path = `/api/v1/saves?gameId=${gameId}&limit=100`;
  const previous = new Set((await client.json("GET", path)).items.map(item => item.saveStateId));
  await opened.frame.evaluate(() => {
    const manager = window.EJS_emulator.gameManager;
    for (const name of ["getState", "getStateAsync"]) {
      const original = manager[name]; if (!original) {continue;}
      const observe = bytes => {
        const copy = new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength).slice();
        window.__flycastRawHash = crypto.subtle.digest("SHA-256", copy).then(digest =>
          Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, "0")).join(""));
        return bytes;
      };
      manager[name] = function (...args) {const value = Reflect.apply(original, this, args); return value?.then ? value.then(observe) : observe(value);};
    }
  });
  await revealPreviewToolbar(opened.page);
  const pause = opened.page.getByRole("button", {name: "暂停", exact: true});
  if (await pause.isVisible()) {await pause.click();}
  await opened.page.waitForTimeout(100);
  await opened.canvas.screenshot({path: join(directory, "before-save.png")});
  await revealPreviewToolbar(opened.page);
  const response = opened.page.waitForResponse(item => item.request().method() === "POST" &&
    new URL(item.url()).pathname === `/runtime/launches/${launchId}/save-states`, {timeout: 60000});
  await opened.page.getByRole("button", {name: "创建存档", exact: true}).click();
  assert.equal((await response).status(), 201);
  const added = (await client.json("GET", path)).items.filter(item => !previous.has(item.saveStateId));
  assert.equal(added.length, 1);
  const screenshot = await client.raw("GET", added[0].screenshotUrl);
  assert.equal(screenshot.status(), 200); writeFileSync(join(directory, "uploaded-screenshot.png"), await screenshot.body());
  return {...added[0], rawSha256: await opened.frame.evaluate(() => window.__flycastRawHash)};
}
try {
  assert.ok([base, env.RETROM_FLYCAST_CHD, env.RETROM_FLYCAST_BIOS_DIR, env.RETROM_CHROME_EXECUTABLE,
    env.RETROM_ACCEPTANCE_USERNAME, env.RETROM_ACCEPTANCE_PASSWORD].every(Boolean), "FLYCAST_INPUT_REQUIRED");
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--autoplay-policy=no-user-gesture-required"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(30000); await installVirtualStandardGamepad(context);
  const client = await fantasyClient(context, base), progress = await prepare(client);
  evidence.gameId = progress.gameId; evidence.gameSha256 = progress.digest;
  const original = await launchCart(client, progress.gameId), first = await open(original);
  await first.page.waitForTimeout(15000);
  for (let i = 0; i < 4; i++) {await gamepad(first.page, 9, 200); await first.page.waitForTimeout(2000);}
  await gamepad(first.page, 13, 180); await gamepad(first.page, 0, 200);
  await first.page.waitForTimeout(5000);
  const saved = await save(first, client, progress.gameId, original.launchId);
  assert.equal(saved.checkpointFormat, "flycast-state-v1-storage-v1");
  evidence.saveStateId = saved.saveStateId; await first.page.close();
  const restored = await launchCart(client, progress.gameId, saved.saveStateId), next = await open(restored);
  assert.notEqual(restored.launchId, original.launchId);
  const response = await client.raw("GET", next.config.restore.url); assert.equal(response.status(), 200);
  const bytes = await response.body(), raw = gunzipSync(bytes, {maxOutputLength: 256 * 1024 * 1024});
  assert.equal(bytes.length, next.config.restore.sizeBytes); assert.equal(hash(bytes), next.config.restore.sha256);
  assert.equal(hash(raw), saved.rawSha256); assert.equal(raw.subarray(0, 7).toString(), "RASTATE");
  assert.ok(bytes.length < raw.length / 2);
  await next.canvas.screenshot({path: join(directory, "restored.png")});
  await gamepad(next.page, 15, 600); await gamepad(next.page, 0, 300);
  await next.canvas.screenshot({path: join(directory, "restored-input.png")}); await next.page.close();
  const fresh = await open(await launchCart(client, progress.gameId));
  assert.equal(fresh.config.restore, null); await fresh.canvas.screenshot({path: join(directory, "fresh.png")}); await fresh.page.close();
  evidence.checkpoint = {storedBytes: bytes.length, rawBytes: raw.length, rawSha256: hash(raw), storedSha256: hash(bytes)};
  evidence.launches = {original: original.launchId, restored: restored.launchId};
  assert.deepEqual(evidence.errors, []); evidence.status = "AWAITING_VISUAL_REVIEW";
} catch (error) {evidence.error = error.message.slice(0, 500); process.exitCode = 1;}
finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "flycast-storage-product.json"), JSON.stringify(evidence, null, 2));
  console.log(JSON.stringify(evidence));
}
