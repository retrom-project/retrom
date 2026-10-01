import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {existsSync, mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {basename, join, resolve} from "node:path";
import {gunzipSync} from "node:zlib";
import {chromium, devices} from "../../web/node_modules/playwright/index.mjs";
import {fantasyClient, previewCart, approveCart, gamepad, saveCart} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {px68kLocalProxy, canvasDigest} from "./px68k_product_support.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {mameParentCacheProbe} from "./mame_parent_cache.mjs";

const env = process.env, baseUrl = env.RETROM_ACCEPTANCE_BASE_URL;
const source = env.RETROM_MAME_ARCADE_ROM, machine = basename(source ?? "", ".zip").toLowerCase();
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/mame-arcade-product");
assert.ok(baseUrl && source && env.RETROM_CHROME_EXECUTABLE && /^[a-z0-9_]{1,32}$/u.test(machine), "MAME_ARCADE_ACCEPTANCE_INPUT_REQUIRED");
mkdirSync(directory, {recursive: true});
const evidence = {caseId: "ACC-MAME-004", machine, status: "FAIL", stages: [], requests: [], contentRequests: [], errors: [], console: []};
const progressPath = join(directory, "progress.json");
let browser, proxy;
try {
  proxy = await px68kLocalProxy(baseUrl);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  evidence.browser = browser.version();
  const display = env.RETROM_MAME_ARCADE_MOBILE === "1"
    ? {...devices["Pixel 5"], viewport: {width: 844, height: 390}}
    : {viewport: {width: 1280, height: 900}};
  evidence.display = {viewport: display.viewport, mobileEmulation: display.isMobile ?? false};
  const context = await browser.newContext({...display, ...proxy.contextOptions});
  context.setDefaultTimeout(30000);
  await installVirtualStandardGamepad(context);
  context.on("response", async response => {
    const path = new URL(response.url()).pathname;
    if (path.startsWith("/runtime/content/") && response.request().method() === "GET") {
      evidence.contentRequests.push({path, status: response.status()});
    }
    if (path.includes("/assets/mame/")) {
      const headers = await response.allHeaders();
      evidence.requests.push({path, status: response.status(), encoding: headers["content-encoding"] ?? "identity",
        wireBytes: Number(headers["content-length"] ?? 0)});
    }
  });
  const client = await fantasyClient(context, baseUrl);
  if (machine === "vf2") {await installModel2DeviceBIOS(client);}
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  const instances = await client.json("GET", "/api/v1/admin/platform-instances?platformId=arcade&limit=100");
  const instance = instances.items.find(item => item.enabled && item.defaultCoreId === "mame_arcade");
  assert.ok(instance, "MAME_ARCADE_DIRECTORY_MISSING");
  const digest = createHash("sha256").update(readFileSync(source)).digest("hex");
  const progress = existsSync(progressPath) ? JSON.parse(readFileSync(progressPath, "utf8")) : {};
  if (progress.digest) {assert.equal(progress.digest, digest, "MAME_ARCADE_ROM_CHANGED");}
  let {itemId, gameId} = progress;
  if (!itemId) {
    const files = [...singleFile(source), ...(env.RETROM_MAME_ARCADE_PARENT ? singleFile(env.RETROM_MAME_ARCADE_PARENT) : [])];
    const uploadId = await client.upload(files, "FILES", "GENERAL");
    const imported = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
      data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []}});
    itemId = (await reviewForImport(client, imported.importJobId)).itemId;
    writeFileSync(progressPath, JSON.stringify({digest, itemId}));
  }
  if (!gameId) {
    evidence.stages.push("import-review");
    const preview = await open(context, await previewCart(client, itemId), "preview");
    await preview.page.waitForTimeout(machine === "vf2" ? 40000 : 8000);
    evidence.preview = await canvasDigest(preview.canvas);
    await preview.canvas.screenshot({path: join(directory, "preview.png")});
    await preview.page.close();
    gameId = (await approveCart(client, itemId)).gameId;
    writeFileSync(progressPath, JSON.stringify({digest, itemId, gameId}));
    evidence.stages.push("preview-publish");
  } else {evidence.stages.push("reuse-published-game");}
  evidence.gameId = gameId;
  const parentCache = env.RETROM_MAME_ARCADE_PARENT ? await mameParentCacheProbe(context, baseUrl) : null;
  const launch = await launchArcade(client, gameId, env.RETROM_MAME_ARCADE_SAVE_STATE ?? null);
  const opened = await open(context, launch, "product");
  if (env.RETROM_MAME_ARCADE_SAVE_STATE) {
    assert.ok(opened.config.restore, "MAME_ARCADE_INITIAL_RESTORE_REQUIRED");
    evidence.initialSaveStateId = env.RETROM_MAME_ARCADE_SAVE_STATE;
  }
  parentCache?.cold(opened.config);
  evidence.runtime = opened.config.runtime;
  await opened.page.waitForTimeout(machine === "vf2" ? 40000 : 8000); // Model 2's first boot takes longer.
  evidence.beforeInput = await canvasDigest(opened.canvas);
  await opened.canvas.screenshot({path: join(directory, "before-input.png")});
  await gamepad(opened.page, 8, 300); // Coin
  if (machine === "vf2") {await gamepad(opened.page, 8, 300);} // VF2 needs two credits.
  await opened.page.waitForTimeout(500);
  evidence.afterCoin = await canvasDigest(opened.canvas);
  await opened.canvas.screenshot({path: join(directory, "after-coin.png")});
  await gamepad(opened.page, 9, 300); // Start
  await opened.page.waitForTimeout(machine === "vf2" ? 8000 : machine === "dkong" ? 9000 : machine === "pacman" ? 5000 : 1800);
  evidence.afterStart = await canvasDigest(opened.canvas);
  await opened.canvas.screenshot({path: join(directory, "after-start.png")});
  await gamepad(opened.page, 15, 800); // Right
  await gamepad(opened.page, 0, 200); // Action 1
  evidence.afterInput = await canvasDigest(opened.canvas);
  await opened.canvas.screenshot({path: join(directory, "after-input.png")});
  if (machine === "vf2") {
    await gamepad(opened.page, 0, 300); // Confirm the selected fighter.
    await opened.page.waitForTimeout(15000);
    evidence.afterConfirm = await canvasDigest(opened.canvas);
    await opened.canvas.screenshot({path: join(directory, "after-confirm.png")});
  }
  assert.notEqual(evidence.beforeInput.sha256, evidence.afterInput.sha256, "MAME_ARCADE_INPUT_UNOBSERVABLE");
  assert.notEqual(evidence.afterStart.sha256, evidence.afterInput.sha256, "MAME_ARCADE_DIRECTION_UNOBSERVABLE");
  if (machine === "vf2") {
    await opened.page.getByRole("status").filter({hasText: "当前场景暂不可存档"}).waitFor();
    await opened.page.close();
    evidence.stages.push("coin-start-direction-action-checkpoint-unsupported");
  } else {
    const saved = await saveCart(opened.page, launch.launchId, "mame_arcade");
    assert.equal(saved.checkpointFormat, "mame-state-v1-storage-v1");
    evidence.saveStateId = saved.saveStateId;
    await opened.page.close();
    evidence.stages.push("coin-start-direction-action-save");
    const resumed = await open(context, await launchArcade(client, gameId, saved.saveStateId), "restore");
    if (parentCache) {evidence.parentCache = parentCache.restored(resumed.config);}
    assert.equal(resumed.config.restore?.format, saved.checkpointFormat);
    const stored = await client.raw("GET", resumed.config.restore.url);
    assert.equal(stored.status(), 200);
    const packed = await stored.body();
    assert.equal(createHash("sha256").update(packed).digest("hex"), resumed.config.restore.sha256);
    assert.equal(gunzipSync(packed, {maxOutputLength: 64 * 1024 * 1024}).subarray(0, 8).toString(), "RTMAME01");
    evidence.restored = await canvasDigest(resumed.canvas);
    await resumed.canvas.screenshot({path: join(directory, "restored.png")});
    await gamepad(resumed.page, 14, 800); // Left after restore
    evidence.restoredAfterInput = await canvasDigest(resumed.canvas);
    assert.notEqual(evidence.restored.sha256, evidence.restoredAfterInput.sha256, "MAME_ARCADE_RESTORED_INPUT_UNOBSERVABLE");
    await resumed.canvas.screenshot({path: join(directory, "restored-after-input.png")});
    await resumed.page.close();
    evidence.stages.push("fresh-launch-restore-input");
  }
  assert.deepEqual(evidence.errors, []);
  evidence.status = "AUTOMATED_PASS_REQUIRES_VISUAL_REVIEW";
} catch (error) {evidence.errorCode = error.message; process.exitCode = 1;}
finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify({status: evidence.status, error: evidence.errorCode}));
}

async function installModel2DeviceBIOS(client) {
  const catalog = await client.json("GET", "/api/v1/admin/bios?scope=FULL_CATALOG&coreId=mame_arcade&limit=100");
  const item = catalog.items.find(entry => entry.logicalName === "epr-18022.ic2" && entry.coreId === "mame_arcade");
  assert.ok(item, "MAME_MODEL2_DEVICE_BIOS_MISSING");
  if (item.status === "MATCHED") {return;}
  const path = env.RETROM_MAME_ARCADE_DEVICE_BIOS;
  assert.ok(path, "MAME_MODEL2_DEVICE_BIOS_INPUT_REQUIRED");
  const uploadId = await client.upload(singleFile(path), "FILES", "GENERAL");
  const upload = await client.json("GET", `/api/v1/admin/uploads/${uploadId}`);
  const response = await client.raw("POST", `/api/v1/admin/bios/${item.id}/installations`, {
    headers: {...client.writeHeaders(), "If-Match": `"v${item.version}"`}, data: {uploadFileId: upload.files[0].fileId},
  });
  assert.equal(response.status(), 201, `MAME_MODEL2_DEVICE_BIOS_INSTALL_FAILED:${response.status()}`);
}

async function launchArcade(client, gameId, saveStateId = null) {
  const data = {gameId, coreId: null, saveStateId, dosEntry: null, returnTo: `/games/${gameId}`,
    clientCapabilities: {secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true}};
  for (let attempt = 0; attempt < 3; attempt++) {
    const response = await client.raw("POST", "/api/v1/launches", {headers: client.writeHeaders(), data});
    if (response.status() === 201) {return response.json();}
    if (response.status() !== 202) {throw Error(`MAME_ARCADE_LAUNCH_HTTP_${response.status()}:${(await response.text()).slice(0, 500)}`);}
    const pending = await response.json();
    assert.ok(pending.jobId, "MAME_ARCADE_VALIDATION_JOB_MISSING");
    for (let poll = 0; poll < 300; poll++) {
      const job = await client.json("GET", `/api/v1/admin/jobs/${pending.jobId}`);
      if (job.state === "SUCCEEDED") {break;}
      if (job.state === "FAILED" || job.state === "CANCELLED") {throw Error(`MAME_ARCADE_VALIDATION_${job.state}:${JSON.stringify(job)}`);}
      await new Promise(resolve => setTimeout(resolve, 100));
    }
  }
  throw Error("MAME_ARCADE_VALIDATION_TIMEOUT");
}

async function open(context, launch, stage) {
  const page = await context.newPage();
  const cdp = await context.newCDPSession(page);
  await cdp.send("Network.enable"); await cdp.send("Network.setCacheDisabled", {cacheDisabled: true});
  page.on("pageerror", error => evidence.errors.push({stage, message: error.message.slice(0, 300)}));
  page.on("console", message => {
    if (message.type() === "error" || message.type() === "warning") {
      evidence.console.push({stage, type: message.type(), text: message.text().slice(0, 600)});
    }
  });
  await page.goto(baseUrl + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
  const deadline = Date.now() + 120000;
  while (Date.now() < deadline) {
    const alerts = await page.locator("[role=alert]").allTextContents();
    const error = alerts.join(" ").match(/\b(?:MAME|PLAYER|PROVIDER|RUNTIME)_[A-Z0-9_]+\b/u)?.[0];
    if (error || await page.getByText("RUNTIME_FAILED", {exact: true}).isVisible()) {
      await page.screenshot({path: join(directory, `failed-${stage}.png`)});
      throw Error(error ?? "MAME_ARCADE_BOOT_FAILED");
    }
    for (const frame of page.frames()) {
      const canvas = frame.locator(`canvas[aria-label="MAME Arcade (${machine})"]`);
      if (!await canvas.isVisible()) {continue;}
      await page.getByRole("status").filter({hasText: machine === "vf2" ? "当前场景暂不可存档" : "可创建存档"}).waitFor({state: "attached", timeout: 30000});
      const id = launch.launchId ?? launch.previewId;
      const config = await page.evaluate(async value => (await fetch(`/runtime/launches/${value}/config`)).json(), id);
      assert.equal(config.runtime.targetId, "mame-arcade");
      assert.equal(config.targetOptions.machine, machine);
      await canvas.click();
      if (["pacman", "mspacman", "dkong"].includes(machine)) {
        const dimensions = await canvas.evaluate(element => ({width: element.width, height: element.height}));
        assert.ok(dimensions.height > dimensions.width, "MAME_ARCADE_PORTRAIT_ROTATION_MISSING");
      }
      for (let count = 0; count < 40; count++) {
        if ((await canvasDigest(canvas)).colors > 2) {return {page, canvas, config};}
        await page.waitForTimeout(500);
      }
      throw Error("MAME_ARCADE_EMPTY_FRAME");
    }
    await page.waitForTimeout(100);
  }
  await page.screenshot({path: join(directory, `failed-${stage}.png`)});
  throw Error("MAME_ARCADE_BOOT_TIMEOUT");
}
