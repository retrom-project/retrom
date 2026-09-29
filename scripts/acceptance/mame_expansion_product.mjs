import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {existsSync, mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {gunzipSync} from "node:zlib";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {fantasyClient, gamepad, previewCart, saveCart} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {px68kLocalProxy, canvasDigest} from "./px68k_product_support.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";

const cases = {
  apple2e: {platformId: "apple2", core: "mame_apple2e", target: "mame-apple2e", family: "apple", label: "Apple IIe (MAME)", file: "Donkey Kong.dsk", bios: 6, startKey: "Space", startupWaitMs: 16000},
  sg1000: {core: "mame_sg1000", target: "mame-sg1000", family: "sg1000", label: "SG-1000 (MAME)", file: "Bank Panic.sg", bios: 0, start: 0, startupWaitMs: 18000},
  colecovision: {core: "mame_coleco", target: "mame-coleco", family: "coleco", label: "ColecoVision (MAME)", file: "Lady Bug.col", bios: 1, defaultBiosCore: "gearcoleco", start: 9, startupWaitMs: 16000, checkpoint: false},
};
const baseUrl = process.env.RETROM_ACCEPTANCE_BASE_URL;
const source = process.env.RETROM_MAME_EXPANSION_INPUT_DIR;
const directory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/mame-expansion-product");
const platforms = process.argv[2] ? [process.argv[2]] : Object.keys(cases);
assert.ok(baseUrl && source && process.env.RETROM_CHROME_EXECUTABLE && platforms.every(name => cases[name]), "MAME_EXPANSION_INPUT_REQUIRED");
mkdirSync(directory, {recursive: true});
const result = {status: "FAIL", cases: [], requests: [], contentRequests: []};
let browser, proxy;
try {
  proxy = await px68kLocalProxy(baseUrl);
  browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(30000);
  await installVirtualStandardGamepad(context);
  context.on("response", async response => {
    const path = new URL(response.url()).pathname;
    if (path.startsWith("/runtime/content/") && response.request().method() === "GET") {
      result.contentRequests.push({path, status: response.status()});
    }
    if (path.includes("/assets/mame/")) {
      const headers = await response.allHeaders();
      result.requests.push({path, status: response.status(), encoding: headers["content-encoding"] ?? "identity",
        wireBytes: Number(headers["content-length"] ?? 0)});
    }
  });
  const client = await fantasyClient(context, baseUrl);
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  for (const platform of platforms) {
    const evidence = {platform, core: cases[platform].core, status: "FAIL", stages: [], errors: []};
    result.cases.push(evidence);
    const caseDirectory = join(directory, platform); mkdirSync(caseDirectory, {recursive: true});
    try {await runCase(client, context, platform, caseDirectory, evidence); evidence.status = "AUTOMATED_PASS_REQUIRES_VISUAL_REVIEW";}
    finally {writeFileSync(join(caseDirectory, "product.json"), JSON.stringify(evidence, null, 2) + "\n");}
  }
  for (const name of ["mame-common.wasm", ...platforms.map(platform => `mame-${cases[platform].family}.wasm`)]) {
    const records = result.requests.filter(item => item.path.endsWith(name));
    assert.equal(records.length, 1, `MAME_CODE_DOWNLOADED_AGAIN:${name}`);
    assert.equal(records[0].encoding, "br");
  }
  assert.ok(result.contentRequests.every(item => item.status === 200));
  result.status = "AUTOMATED_PASS_REQUIRES_VISUAL_REVIEW";
} catch (error) {result.errorCode = error.message; process.exitCode = 1;}
finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "product.json"), JSON.stringify(result, null, 2) + "\n");
  console.log(JSON.stringify({status: result.status, error: result.errorCode}));
}

async function runCase(client, context, platform, caseDirectory, evidence) {
  const profile = cases[platform];
  for (const core of [profile.core, ...(profile.defaultBiosCore ? [profile.defaultBiosCore] : [])]) {
    const catalog = await client.json("GET", `/api/v1/admin/bios?scope=FULL_CATALOG&coreId=${core}&limit=100`);
    const requirements = catalog.items.filter(item => item.coreId === core);
    assert.equal(requirements.length, core === profile.core ? profile.bios : 1, "MAME_EXPANSION_BIOS_CATALOG_MISMATCH");
    for (const item of requirements) {
      if (item.status === "MATCHED") {continue;}
      const uploadId = await client.upload(singleFile(join(source, item.logicalName)), "FILES", "GENERAL");
      const upload = await client.json("GET", `/api/v1/admin/uploads/${uploadId}`);
      const response = await client.raw("POST", `/api/v1/admin/bios/${item.id}/installations`, {
        headers: {...client.writeHeaders(), "If-Match": `"v${item.version}"`}, data: {uploadFileId: upload.files[0].fileId},
      });
      assert.equal(response.status(), 201, `MAME_EXPANSION_BIOS_INSTALL_FAILED:${core}:${item.logicalName}`);
    }
  }
  evidence.stages.push("bios");
  const instances = await client.json("GET", `/api/v1/admin/platform-instances?platformId=${profile.platformId ?? platform}&limit=100`);
  const directories = instances.items.filter(item => item.enabled);
  assert.equal(directories.length, 1, "MAME_EXPANSION_PLATFORM_DIRECTORY_COUNT");
  evidence.platformInstanceId = directories[0].id;
  evidence.defaultCoreId = directories[0].defaultCoreId;
  const path = join(source, profile.file), digest = createHash("sha256").update(readFileSync(path)).digest("hex");
  const progressPath = join(caseDirectory, "progress.json");
  const progress = existsSync(progressPath) ? JSON.parse(readFileSync(progressPath, "utf8")) : {};
  if (progress.digest) {assert.equal(progress.digest, digest, "MAME_EXPANSION_GAME_CHANGED");}
  let {itemId, gameId} = progress;
  if (gameId) {evidence.stages.push("reused-published-game");}
  if (!itemId) {
    const uploadId = await client.upload(singleFile(path), "FILES", "GENERAL");
    const imported = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
      data: {uploadId, targetPlatformInstanceId: directories[0].id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []}});
    itemId = (await reviewForImport(client, imported.importJobId)).itemId;
    writeFileSync(progressPath, JSON.stringify({digest, itemId}));
  }
  if (!gameId) {
    await previewCart(client, itemId);
    const response = await client.raw("GET", `/api/v1/admin/reviews/${itemId}`);
    assert.equal(response.status(), 200);
    const approved = await client.json("POST", `/api/v1/admin/reviews/${itemId}/approve`, {
      headers: {...client.writeHeaders(), "If-Match": response.headers().etag}, data: {}, expected: 201,
    });
    gameId = approved.gameId;
    writeFileSync(progressPath, JSON.stringify({digest, itemId, gameId}));
  }
  evidence.gameId = gameId; evidence.gameSha256 = digest;
  if (!progress.gameId) {evidence.stages.push("import-review-publish");}
  const first = await launch(client, gameId, profile.core);
  const opened = await open(context, baseUrl, first, profile, caseDirectory, evidence, "product");
  evidence.runtime = opened.config.runtime;
  await opened.page.waitForTimeout(profile.startupWaitMs);
  evidence.beforeInput = await canvasDigest(opened.canvas);
  await opened.canvas.screenshot({path: join(caseDirectory, "before-input.png")});
  if (profile.startKey) {
    await opened.page.keyboard.down(profile.startKey);
    await opened.page.waitForTimeout(300);
    await opened.page.keyboard.up(profile.startKey);
  } else {await gamepad(opened.page, profile.start, 300);}
  await opened.page.waitForTimeout(5000);
  await gamepad(opened.page, 15, 800);
  await gamepad(opened.page, 0, 200);
  if (platform === "sg1000") {
    await gamepad(opened.page, 0, 350); // Confirm the level after Bank Panic's start screen.
    await opened.page.waitForTimeout(15000);
  }
  evidence.afterInput = await canvasDigest(opened.canvas);
  await opened.canvas.screenshot({path: join(caseDirectory, "after-input.png")});
  assert.ok(evidence.afterInput.colors > 2, "MAME_EXPANSION_EMPTY_FRAME");
  assert.notEqual(evidence.afterInput.sha256, evidence.beforeInput.sha256, "MAME_EXPANSION_INPUT_UNOBSERVABLE");
  if (platform === "sg1000") {
    await gamepad(opened.page, 0, 350);
    await opened.page.waitForTimeout(4000);
    evidence.beforeSaveAction = await canvasDigest(opened.canvas);
    await opened.canvas.screenshot({path: join(caseDirectory, "before-save-action.png")});
    assert.notEqual(evidence.beforeSaveAction.sha256, evidence.afterInput.sha256, "MAME_EXPANSION_INPUT_UNOBSERVABLE");
  }
  if (profile.checkpoint === false) {
    await opened.page.close();
    evidence.stages.push("product-input-checkpoint-unsupported");
    assert.deepEqual(evidence.errors, []);
    return;
  }
  const saved = await saveCart(opened.page, first.launchId, profile.core);
  assert.equal(saved.checkpointFormat, "mame-state-v1-storage-v1");
  evidence.saveStateId = saved.saveStateId;
  await opened.page.close();
  evidence.stages.push("product-input-save");
  const resumed = await launch(client, gameId, profile.core, saved.saveStateId);
  const restored = await open(context, baseUrl, resumed, profile, caseDirectory, evidence, "restore");
  assert.equal(restored.config.restore?.format, saved.checkpointFormat);
  const stored = await client.raw("GET", restored.config.restore.url);
  assert.equal(stored.status(), 200);
  const packed = await stored.body();
  assert.equal(createHash("sha256").update(packed).digest("hex"), restored.config.restore.sha256);
  assert.equal(gunzipSync(packed, {maxOutputLength: 64 * 1024 * 1024}).subarray(0, 8).toString(), "RTMAME01");
  await restored.page.waitForTimeout(profile.restoreWaitMs ?? 3000);
  evidence.restored = await canvasDigest(restored.canvas);
  await restored.canvas.screenshot({path: join(caseDirectory, "restored.png")});
  await gamepad(restored.page, 14, 600);
  if (platform === "sg1000") {
    await gamepad(restored.page, 0, 350); // Fire at a door after restoring Bank Panic.
    await restored.page.waitForTimeout(4000);
  }
  for (let count = 0; count < 12; count++) {
    await restored.page.waitForTimeout(500);
    evidence.restoredAfterInput = await canvasDigest(restored.canvas);
    if (evidence.restoredAfterInput.colors > 2) {break;}
  }
  await restored.canvas.screenshot({path: join(caseDirectory, "restored-after-input.png")});
  await restored.page.close();
  assert.ok(evidence.restoredAfterInput.colors > 2, "MAME_EXPANSION_RESTORE_EMPTY_FRAME");
  if (platform === "sg1000") {
    assert.notEqual(evidence.restoredAfterInput.sha256, evidence.restored.sha256, "MAME_EXPANSION_RESTORE_INPUT_UNOBSERVABLE");
  }
  assert.deepEqual(evidence.errors, []);
  evidence.stages.push("fresh-launch-restore-input");
}

async function launch(client, gameId, coreId, saveStateId = null) {
  const data = {gameId, coreId, saveStateId, dosEntry: null, returnTo: `/games/${gameId}`,
    clientCapabilities: {secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true}};
  for (let attempt = 0; attempt < 3; attempt++) {
    const response = await client.raw("POST", "/api/v1/launches", {headers: client.writeHeaders(), data});
    if (response.status() === 201) {return response.json();}
    if (response.status() !== 202) {throw Error(`MAME_EXPANSION_LAUNCH_HTTP_${response.status()}:${(await response.text()).slice(0, 500)}`);}
    const pending = await response.json();
    assert.ok(pending.jobId, "MAME_EXPANSION_VALIDATION_JOB_MISSING");
    for (let poll = 0; poll < 300; poll++) {
      const job = await client.json("GET", `/api/v1/admin/jobs/${pending.jobId}`);
      if (job.state === "SUCCEEDED") {break;}
      if (job.state === "FAILED" || job.state === "CANCELLED") {throw Error(`MAME_EXPANSION_VALIDATION_${job.state}:${JSON.stringify(job)}`);}
      await new Promise(resolve => setTimeout(resolve, 100));
    }
  }
  throw Error("MAME_EXPANSION_VALIDATION_TIMEOUT");
}

async function open(context, baseUrl, launch, profile, caseDirectory, evidence, stage) {
  const page = await context.newPage();
  const cdp = await context.newCDPSession(page);
  await cdp.send("Network.enable"); await cdp.send("Network.setCacheDisabled", {cacheDisabled: true});
  page.on("pageerror", error => evidence.errors.push({stage, message: error.message.slice(0, 300)}));
  await page.goto(baseUrl + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
  const deadline = Date.now() + 120000;
  while (Date.now() < deadline) {
    const alert = (await page.locator("[role=alert]").allTextContents()).join(" ");
    const error = alert.match(/\b(?:MAME|PLAYER|PROVIDER|RUNTIME)_[A-Z0-9_]+\b/u)?.[0];
    if (error || await page.getByText("RUNTIME_FAILED", {exact: true}).isVisible()) {
      await page.screenshot({path: join(caseDirectory, `failed-${stage}.png`)});
      throw Error(error ?? "MAME_EXPANSION_BOOT_FAILED");
    }
    for (const frame of page.frames()) {
      const canvas = frame.locator(`canvas[aria-label="${profile.label}"]`);
      if (!await canvas.isVisible()) {continue;}
      try {
        await page.getByRole("status").filter({hasText: profile.checkpoint === false ? "当前场景暂不可存档" : "可创建存档"}).waitFor({state: "attached", timeout: 30000});
      } catch (error) {
        await page.screenshot({path: join(caseDirectory, `failed-status-${stage}.png`)});
        throw Error(`MAME_EXPANSION_CHECKPOINT_STATUS:${JSON.stringify(await page.getByRole("status").allTextContents())}:${error.message}`);
      }
      const config = await page.evaluate(async id => (await fetch(`/runtime/launches/${id}/config`)).json(), launch.launchId);
      assert.equal(config.runtime.targetId, profile.target);
      await canvas.click();
      for (let count = 0; count < 40; count++) {
        if ((await canvasDigest(canvas)).colors > 2) {return {page, canvas, config};}
        await page.waitForTimeout(500);
      }
      throw Error("MAME_EXPANSION_EMPTY_FRAME");
    }
    await page.waitForTimeout(100);
  }
  await page.screenshot({path: join(caseDirectory, `failed-${stage}.png`)});
  throw Error("MAME_EXPANSION_BOOT_TIMEOUT");
}
