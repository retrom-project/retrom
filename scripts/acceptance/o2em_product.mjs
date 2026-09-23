import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {existsSync, mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {gunzipSync} from "node:zlib";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import sharp from "../../web/node_modules/sharp/dist/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, approveCart, launchCart, gamepad} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {revealPreviewToolbar, resumePreview} from "./rpgmaker_preview_actions.mjs";

const env = process.env;
const base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/o2em-product");
const evidence = {schemaVersion: 1, caseId: "ACC-O2EM-001", status: "FAIL", stages: [], errors: [], runtimes: []};
const hash = bytes => createHash("sha256").update(bytes).digest("hex");
mkdirSync(directory, {recursive: true});
let browser, proxy;

async function installBios(client) {
  const requirements = (await client.json("GET", "/api/v1/admin/bios?scope=FULL_CATALOG&coreId=o2em&limit=100")).items;
  const item = requirements.find(entry => entry.coreId === "o2em" && entry.logicalName === "o2rom.bin");
  assert.ok(item, "O2EM_BIOS_REQUIREMENT_MISSING");
  if (["MATCHED", "HASH_WARNING"].includes(item.status)) {
    assert.equal(item.status, "MATCHED", "O2EM_BIOS_HASH_WARNING");
    return;
  }
  assert.equal(item.activeInstallation, null, "O2EM_BIOS_INSTALLATION_UNEXPECTED");
  const uploadId = await client.upload(singleFile(env.RETROM_O2EM_BIOS), "FILES", "GENERAL");
  const upload = await client.json("GET", `/api/v1/admin/uploads/${uploadId}`);
  await client.json("POST", `/api/v1/admin/bios/${item.id}/installations`, {
    expected: 201, headers: {...client.writeHeaders(), "If-Match": `"v${item.version}"`},
    data: {uploadFileId: upload.files[0].fileId},
  });
  evidence.stages.push("bios-installed");
}

async function prepare(client) {
  await installBios(client);
  const path = join(directory, "product-input.json");
  const progress = existsSync(path) ? JSON.parse(readFileSync(path, "utf8")) : {};
  const digest = hash(readFileSync(env.RETROM_O2EM_ROM));
  if (progress.romSha256) {assert.equal(progress.romSha256, digest, "O2EM_ROM_CHANGED");}
  if (!progress.review) {
    await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
      headers: client.writeHeaders(), data: {}, expected: 200,
    });
    const platforms = await client.json("GET", "/api/v1/admin/platform-instances?platformId=odyssey2&limit=100");
    const instance = platforms.items.find(entry => entry.enabled && entry.defaultCoreId === "o2em");
    assert.ok(instance, "O2EM_PLATFORM_MISSING");
    const uploadId = await client.upload(singleFile(env.RETROM_O2EM_ROM), "FILES", "GENERAL");
    const imported = await client.json("POST", "/api/v1/admin/imports", {
      headers: client.writeHeaders(), expected: 202,
      data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []},
    });
    progress.review = await reviewForImport(client, imported.importJobId);
    progress.romSha256 = digest;
    writeFileSync(path, JSON.stringify(progress, null, 2) + "\n");
  }
  evidence.itemId = progress.review.itemId;
  evidence.romSha256 = digest;
  const reused = Boolean(progress.gameId);
  evidence.stages.push(reused ? "published-game-reused" : "import-review");
  if (!progress.gameId) {
    const preview = await open(await previewCart(client, progress.review.itemId));
    await preview.page.waitForTimeout(2000);
    await picture(preview, "review-preview");
    await preview.page.close();
    evidence.stages.push("review-preview");
    progress.gameId = (await approveCart(client, progress.review.itemId)).gameId;
    writeFileSync(path, JSON.stringify(progress, null, 2) + "\n");
  }
  evidence.gameId = progress.gameId;
  if (!reused) {evidence.stages.push("publish");}
  return progress.gameId;
}

async function open(launch, {pauseOnReady = false} = {}) {
  const id = launch.launchId ?? launch.previewId;
  const page = await browser.contexts()[0].newPage();
  page.on("pageerror", error => evidence.errors.push(error.message.split("\n")[0].slice(0, 200)));
  page.on("console", message => {
    if (["error", "warning"].includes(message.type())) {
      (evidence.console ??= []).push(`${message.type()}: ${message.text()}`.slice(0, 300));
    }
  });
  await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
  await page.locator(".player-loading").waitFor({state: "hidden", timeout: 90000});
  const canvas = page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas");
  await canvas.waitFor({state: "visible", timeout: 90000});
  await page.getByRole("status").filter({hasText: "可创建存档"}).waitFor({state: "attached", timeout: 45000});
  if (pauseOnReady) {
    // Let the restored VDC complete a frame before freezing the visible output.
    await page.waitForTimeout(180);
    await revealPreviewToolbar(page);
    await page.getByRole("button", {name: "暂停", exact: true}).click();
  }
  const frame = page.frames().find(entry => entry !== page.mainFrame());
  const config = await page.evaluate(async launchId => (await fetch(`/runtime/launches/${launchId}/config`)).json(), id);
  assert.equal(config.runtime.providerId, "emulatorjs");
  assert.equal(config.runtime.targetId, "o2em");
  const game = config.resources.find(entry => entry.kind === "ROM_BLOB");
  assert.equal(game?.sha256, evidence.romSha256);
  assert.ok(config.resources.some(entry => entry.kind === "BIOS_BUNDLE"), "O2EM_BIOS_NOT_DELIVERED");
  const coreSha256 = await page.evaluate(async runtimeBase => {
    const response = await fetch(runtimeBase + "assets/4.2.3/data/cores/o2em-wasm.data");
    if (!response.ok) {throw Error("O2EM_CORE_FETCH_FAILED");}
    const digest = await crypto.subtle.digest("SHA-256", await response.arrayBuffer());
    return [...new Uint8Array(digest)].map(value => value.toString(16).padStart(2, "0")).join("");
  }, config.runtime.runtimeBaseUrl);
  assert.equal(coreSha256, env.RETROM_O2EM_CORE_SHA256);
  const controls = await frame.evaluate(() => window.EJS_defaultControls);
  assert.equal(controls[0][0].value2, "BUTTON_1");
  assert.equal(controls[1][0].value2, "BUTTON_1");
  evidence.runtimes.push({id, coreSha256, ...config.runtime, runtimeBaseUrl: undefined, moduleUrl: undefined});
  if (!pauseOnReady) {await canvas.click();}
  return {page, frame, canvas, config};
}

async function picture(opened, name) {
  const png = await opened.canvas.screenshot({path: join(directory, `${name}.png`),
    style: ".player-pause-overlay,.player-toolbar,.player-toast{visibility:hidden!important}"});
  const stats = await sharp(png).stats();
  const contrast = Math.max(...stats.channels.slice(0, 3).map(channel => channel.stdev));
  assert.ok(contrast > 5, `O2EM_BLANK_FRAME:${name}`);
  evidence.stages.push(name);
  return {sha256: hash(png), contrast};
}

async function sceneSimilarity(leftName, rightName) {
  const left = await sharp(join(directory, `${leftName}.png`)).raw().toBuffer({resolveWithObject: true});
  const right = await sharp(join(directory, `${rightName}.png`)).raw().toBuffer({resolveWithObject: true});
  assert.equal(left.info.width, right.info.width);
  assert.equal(left.info.height, right.info.height);
  let visible = 0;
  let matched = 0;
  for (let y = 90; y < 790; y += 4) {
    for (let x = 110; x < 1140; x += 4) {
      const a = (y * left.info.width + x) * left.info.channels;
      const b = (y * right.info.width + x) * right.info.channels;
      const first = [left.data[a], left.data[a + 1], left.data[a + 2]];
      const second = [right.data[b], right.data[b + 1], right.data[b + 2]];
      if (first.reduce((sum, value) => sum + value, 0) <= 90 &&
          second.reduce((sum, value) => sum + value, 0) <= 90) {continue;}
      visible += 1;
      if (first.reduce((sum, value, index) => sum + Math.abs(value - second[index]), 0) < 30) {matched += 1;}
    }
  }
  assert.ok(visible > 100, "O2EM_SCENE_EMPTY");
  return matched / visible;
}

async function save(opened, client, gameId, launchId) {
  const path = `/api/v1/saves?gameId=${gameId}&limit=100`;
  const before = new Set((await client.json("GET", path)).items.map(entry => entry.saveStateId));
  await revealPreviewToolbar(opened.page);
  const response = opened.page.waitForResponse(entry => entry.request().method() === "POST" &&
    new URL(entry.url()).pathname === `/runtime/launches/${launchId}/save-states`, {timeout: 60000});
  await opened.page.getByRole("button", {name: "创建存档", exact: true}).click();
  assert.equal((await response).status(), 201, "O2EM_SAVE_HTTP_FAILED");
  const added = (await client.json("GET", path)).items.filter(entry => !before.has(entry.saveStateId));
  assert.equal(added.length, 1, "O2EM_SAVE_RECEIPT_AMBIGUOUS");
  assert.ok(added[0].sizeBytes > 0 && added[0].sizeBytes <= opened.config.runtime.checkpoint.maxBytes);
  return added[0];
}

try {
  if (![base, env.RETROM_O2EM_ROM, env.RETROM_O2EM_BIOS, env.RETROM_O2EM_CORE_SHA256,
    env.RETROM_CHROME_EXECUTABLE, env.RETROM_ACCEPTANCE_USERNAME, env.RETROM_ACCEPTANCE_PASSWORD].every(Boolean)) {
    evidence.status = "BLOCKED"; throw Error("O2EM_ACCEPTANCE_INPUT_REQUIRED");
  }
  evidence.biosSha256 = hash(readFileSync(env.RETROM_O2EM_BIOS));
  assert.equal(evidence.biosSha256, "cb0c5d9ed64f7c1d8870333451832638885b9aa3d7013f0c05fd2a20a5e5bfef");
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--enable-unsafe-swiftshader", "--autoplay-policy=no-user-gesture-required"]});
  evidence.chromeVersion = browser.version();
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(30000);
  await installVirtualStandardGamepad(context);
  const client = await fantasyClient(context, base);
  const gameId = await prepare(client);
  const original = await launchCart(client, gameId);
  const first = await open(original);
  await resumePreview(first.page);
  await first.canvas.click();
  await first.page.keyboard.down("1");
  await first.page.waitForTimeout(350);
  await first.page.keyboard.up("1");
  await first.page.waitForTimeout(400);
  evidence.frames = {initial: await picture(first, "A-initial")};
  await gamepad(first.page, 15, 200);
  evidence.frames.afterInput = await picture(first, "B-after-input");
  await revealPreviewToolbar(first.page);
  await first.page.getByRole("button", {name: "暂停", exact: true}).click();
  evidence.frames.beforeSave = await picture(first, "B-before-save");
  const saved = await save(first, client, gameId, original.launchId);
  evidence.saveStateId = saved.saveStateId;
  await resumePreview(first.page);
  await gamepad(first.page, 14, 200);
  await gamepad(first.page, 0, 150);
  evidence.frames.afterSave = await picture(first, "C-after-save");
  await first.page.close();
  const restored = await launchCart(client, gameId, saved.saveStateId);
  assert.notEqual(restored.launchId, original.launchId);
  const second = await open(restored, {pauseOnReady: true});
  assert.equal(second.config.restore.format, "emulatorjs-state-v1-storage-v1");
  const response = await client.raw("GET", second.config.restore.url);
  assert.equal(response.status(), 200);
  const bytes = await response.body();
  assert.equal(bytes.length, second.config.restore.sizeBytes);
  assert.equal(hash(bytes), second.config.restore.sha256);
  const raw = gunzipSync(bytes, {maxOutputLength: second.config.runtime.checkpoint.maxBytes});
  assert.equal(raw.subarray(0, 7).toString(), "RASTATE");
  evidence.checkpoint = {storedBytes: bytes.length, rawBytes: raw.length, storedSha256: hash(bytes), rawSha256: hash(raw)};
  evidence.frames.restored = await picture(second, "B-restored");
  await resumePreview(second.page);
  await gamepad(second.page, 15, 200);
  evidence.frames.restoredDirection = await picture(second, "D-restored-direction");
  await gamepad(second.page, 0, 150);
  evidence.frames.restoredInput = await picture(second, "D-restored-input");
  await second.page.close();
  const fresh = await open(await launchCart(client, gameId));
  assert.equal(fresh.config.restore, null);
  evidence.frames.fresh = await picture(fresh, "E-fresh");
  await fresh.page.close();
  assert.deepEqual(evidence.errors, []);
  evidence.sceneChecks = {
    restored: await sceneSimilarity("B-before-save", "B-restored"),
    continued: await sceneSimilarity("B-before-save", "D-restored-input"),
    fresh: await sceneSimilarity("B-before-save", "E-fresh"),
  };
  assert.ok(evidence.sceneChecks.restored > 0.7 && evidence.sceneChecks.continued > 0.7,
    "O2EM_RESTORED_SCENE_MISMATCH");
  assert.ok(evidence.sceneChecks.fresh < 0.25, "O2EM_FRESH_LAUNCH_NOT_NEW_GAME");
  assert.notEqual(evidence.frames.initial.sha256, evidence.frames.afterInput.sha256);
  assert.notEqual(evidence.frames.restored.sha256, evidence.frames.restoredInput.sha256);
  evidence.launches = {original: original.launchId, restored: restored.launchId};
  evidence.status = "PASS";
} catch (error) {
  evidence.errorCode = error.message.split("\n")[0].slice(0, 300);
  evidence.errorDetail = error.message.slice(0, 1500);
  process.exitCode = evidence.status === "BLOCKED" ? 3 : 1;
} finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "o2em-product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}
