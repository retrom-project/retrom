import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {mkdirSync, readFileSync, writeFileSync, existsSync} from "node:fs";
import {join, resolve} from "node:path";
import {gunzipSync} from "node:zlib";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {px68kLocalProxy, canvasDigest} from "./px68k_product_support.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, approveCart, launchCart, gamepad, saveCart} from "./fantasy_product_client.mjs";
import {observeAudio, audioEvidence, playerPosition, waitForPlayer, checkConsole, verifyDisplay, verifyPause, screenshotEvidence} from "./mame_product_support.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";

const env = process.env;
const baseUrl = env.RETROM_ACCEPTANCE_BASE_URL;
const biosDirectory = env.RETROM_MAME_APPLE2_BIOS_DIR;
const diskPath = env.RETROM_MAME_APPLE2_DISK;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/mame-apple2-product");
mkdirSync(directory, {recursive: true});
const evidence = {caseId: "ACC-MAME-001", status: "FAIL", stages: [], errors: [], warnings: [], requests: [], contentRequests: []};
const progressPath = join(directory, "progress.json");
let browser, proxy;
try {
  assert.ok(baseUrl && biosDirectory && diskPath && env.RETROM_CHROME_EXECUTABLE, "MAME_APPLE2_ACCEPTANCE_INPUT_REQUIRED");
  proxy = await px68kLocalProxy(baseUrl);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(30000);
  await installVirtualStandardGamepad(context);
  await observeAudio(context);
  context.on("response", async response => {
    const path = new URL(response.url()).pathname;
    if (path.startsWith("/runtime/content/") && response.request().method() === "GET") {
      evidence.contentRequests.push({path, status: response.status()});
    }
    if (response.url().includes("/assets/mame/") && !response.url().startsWith("blob:")) {
      const headers = await response.allHeaders();
      if (path.endsWith("mame-build.json")) {evidence.nativeBuild = (await response.json()).buildId;}
      evidence.requests.push({path: new URL(response.url()).pathname, status: response.status(), encoding: headers["content-encoding"] ?? "identity", wireBytes: headers["content-length"] ?? null});
    }
  });
  const client = await fantasyClient(context, baseUrl);
  const catalog = await client.json("GET", "/api/v1/admin/bios?scope=FULL_CATALOG&coreId=mame_apple2&limit=100");
  const requirements = catalog.items.filter(item => item.coreId === "mame_apple2");
  assert.equal(requirements.length, 9, "MAME_APPLE2_BIOS_CATALOG_MISSING");
  for (const requirement of requirements) {
    if (requirement.status === "MATCHED") {continue;}
    assert.equal(requirement.activeInstallation, null, "MAME_APPLE2_BIOS_INSTALLATION_CONFLICT");
    const uploadId = await client.upload(singleFile(join(biosDirectory, requirement.logicalName)), "FILES", "GENERAL");
    const upload = await client.json("GET", `/api/v1/admin/uploads/${uploadId}`);
    const result = await client.raw("POST", `/api/v1/admin/bios/${requirement.id}/installations`, {
      headers: {...client.writeHeaders(), "If-Match": `"v${requirement.version}"`},
      data: {uploadFileId: upload.files[0].fileId},
    });
    assert.equal(result.status(), 201, `MAME_APPLE2_BIOS_INSTALL_FAILED:${requirement.logicalName}`);
  }
  evidence.stages.push("bios"); console.log("apple2: bios");
  const disk = readFileSync(diskPath);
  const digest = createHash("sha256").update(disk).digest("hex");
  const progress = existsSync(progressPath) ? JSON.parse(readFileSync(progressPath, "utf8")) : {};
  if (progress.digest) {assert.equal(progress.digest, digest, "MAME_APPLE2_ACCEPTANCE_DISK_CHANGED");}
  let {itemId, gameId} = progress;
  if (!itemId) {
    await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
      headers: client.writeHeaders(), data: {}, expected: 200,
    });
    const instances = await client.json("GET", "/api/v1/admin/platform-instances?platformId=apple2&limit=100");
    const instance = instances.items.find(item => item.enabled && item.defaultCoreId === "mame_apple2");
    assert.ok(instance, "MAME_APPLE2_PLATFORM_MISSING");
    const uploadId = await client.upload(singleFile(diskPath), "FILES", "GENERAL");
    const imported = await client.json("POST", "/api/v1/admin/imports", {
      headers: client.writeHeaders(), expected: 202,
      data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []},
    });
    itemId = (await reviewForImport(client, imported.importJobId)).itemId;
    writeFileSync(progressPath, JSON.stringify({digest, itemId, gameId}));
  }
  evidence.stages.push("import"); console.log("apple2: import");
  if (!gameId) {
    const preview = await open(context, await previewCart(client, itemId), "preview");
    await preview.canvas.screenshot({path: join(directory, "preview.png")});
    evidence.preview = await canvasDigest(preview.canvas);
    assert.ok(evidence.preview.colors > 2, "MAME_APPLE2_PREVIEW_EMPTY");
    await preview.page.close();
    gameId = (await approveCart(client, itemId)).gameId;
    writeFileSync(progressPath, JSON.stringify({digest, itemId, gameId}));
    evidence.stages.push("review-preview-publish");
  } else {evidence.stages.push("reused-published-game");}
  console.log("apple2: publish");
  const launch = await launchCart(client, gameId);
  const opened = await open(context, launch, "product");
  evidence.runtime = opened.config.runtime;
  evidence.display = await verifyDisplay(opened.page, opened.canvas);
  await opened.page.waitForTimeout(16000);
  await opened.canvas.screenshot({path: join(directory, "before-input.png")});
  evidence.menu = await canvasDigest(opened.canvas);
  await gamepad(opened.page, 9, 300);
  evidence.playerBefore = await waitForPlayer(opened.canvas);
  await opened.canvas.screenshot({path: join(directory, "after-confirm.png")});
  evidence.pause = await verifyPause(opened.page, opened.canvas);
  await gamepad(opened.page, 15, 700);
  evidence.playerAfterRight = await playerPosition(opened.canvas);
  await opened.canvas.screenshot({path: join(directory, "after-input.png")});
  assert.ok(evidence.playerAfterRight?.x > evidence.playerBefore.x + 5, "MAME_PLAYER_DID_NOT_MOVE_RIGHT");
  await gamepad(opened.page, 0, 100);
  evidence.playerJump = await playerPosition(opened.canvas);
  await opened.canvas.screenshot({path: join(directory, "jump.png")});
  assert.ok(evidence.playerJump?.y < evidence.playerAfterRight.y - 1, "MAME_PLAYER_DID_NOT_JUMP");
  await opened.page.waitForTimeout(500);
  evidence.playerSaved = await playerPosition(opened.canvas);
  assert.ok(evidence.playerSaved, "MAME_SAVE_NOT_IN_GAME");
  await opened.canvas.screenshot({path: join(directory, "saved.png")});
  evidence.audio = await audioEvidence(opened.page);
  evidence.before = await canvasDigest(opened.canvas);
  evidence.beforeLightFraction = await lightFraction(opened.canvas);
  const saved = await saveCart(opened.page, launch.launchId, "mame_apple2");
  assert.equal(saved.checkpointFormat, "mame-state-v1-storage-v1");
  evidence.saveStateId = saved.saveStateId;
  evidence.screenshots = await screenshotEvidence(opened.page);
  await opened.page.close();
  evidence.stages.push("product-input-save"); console.log("apple2: save");
  const resumedLaunch = await launchCart(client, gameId, saved.saveStateId);
  assert.notEqual(resumedLaunch.launchId, launch.launchId);
  const resumed = await open(context, resumedLaunch, "restore");
  assert.equal(resumed.config.restore?.format, "mame-state-v1-storage-v1");
  const stored = await client.raw("GET", resumed.config.restore.url);
  assert.equal(stored.status(), 200, "MAME_APPLE2_SAVE_NOT_READABLE");
  const packed = await stored.body();
  assert.equal(packed.length, resumed.config.restore.sizeBytes);
  assert.equal(createHash("sha256").update(packed).digest("hex"), resumed.config.restore.sha256);
  const rawState = gunzipSync(packed, {maxOutputLength: 64 * 1024 * 1024});
  assert.equal(rawState.subarray(0, 8).toString(), "RTMAME01", "MAME_APPLE2_SAVE_HAS_EXTRA_COMPRESSION_LAYER");
  evidence.checkpointStorage = {packedBytes: packed.length, rawBytes: rawState.length};
  evidence.restoredBeforeInput = await canvasDigest(resumed.canvas);
  evidence.playerRestored = await playerPosition(resumed.canvas);
  await resumed.canvas.screenshot({path: join(directory, "restored-before-input.png")});
  assert.ok(evidence.playerRestored && Math.abs(evidence.playerRestored.x - evidence.playerSaved.x) < 4 &&
    Math.abs(evidence.playerRestored.y - evidence.playerSaved.y) < 4, "MAME_RESTORE_CHARACTER_POSITION_CHANGED");
  await gamepad(resumed.page, 14, 500);
  await resumed.canvas.screenshot({path: join(directory, "restored.png")});
  evidence.restored = await canvasDigest(resumed.canvas);
  evidence.playerAfterLeft = await playerPosition(resumed.canvas);
  assert.ok(evidence.playerAfterLeft?.x < evidence.playerRestored.x - 5, "MAME_RESTORED_PLAYER_DID_NOT_MOVE_LEFT");
  assert.notEqual(evidence.restoredBeforeInput.sha256, evidence.restored.sha256, "MAME_APPLE2_RESTORED_DIRECTION_SCREEN_UNCHANGED");
  evidence.restoredLightFraction = await lightFraction(resumed.canvas);
  assert.ok(Math.abs(evidence.restoredLightFraction - evidence.beforeLightFraction) < 0.15,
    "MAME_APPLE2_RESTORE_VISUAL_CORRUPTION");
  await resumed.page.close();
  evidence.stages.push("fresh-launch-restore-input");
  const common = evidence.requests.filter(item => item.path.endsWith("mame-common.wasm"));
  assert.equal(common.length, 1, "MAME_COMMON_DOWNLOADED_AGAIN");
  assert.equal(common[0].encoding, "br", "MAME_COMMON_NOT_COMPRESSED");
  assert.equal(evidence.contentRequests.length, 10, "MAME_GAME_OR_BIOS_DOWNLOADED_AGAIN");
  assert.equal(new Set(evidence.contentRequests.map(item => item.path)).size, 10);
  assert.ok(evidence.contentRequests.every(item => item.status === 200));
  assert.deepEqual(evidence.errors, []);
  checkConsole(evidence.warnings);
  evidence.status = "AUTOMATED_PASS_REQUIRES_VISUAL_REVIEW";
} catch (error) {
  evidence.errorCode = error.message;
  process.exitCode = 1;
} finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}

async function lightFraction(canvas) {
  return canvas.evaluate(element => {
    const pixels = element.getContext("2d").getImageData(0, 0, element.width, element.height).data;
    let bright = 0;
    for (let i = 0; i < pixels.length; i += 4) {
      if (pixels[i] + pixels[i + 1] + pixels[i + 2] > 650) {bright++;}
    }
    return bright / (pixels.length / 4);
  });
}

async function open(context, launch, stage) {
  const page = await context.newPage();
  const cdp = await context.newCDPSession(page);
  await cdp.send("Network.enable"); await cdp.send("Network.setCacheDisabled", {cacheDisabled: true});
  page.on("console", message => {if (["warning", "error"].includes(message.type())) {evidence.warnings.push({stage, type: message.type(), text: message.text().slice(0, 300)});}});
  page.on("pageerror", error => evidence.errors.push(error.message.slice(0, 200)));
  await page.goto(baseUrl + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
  const deadline = Date.now() + 90000;
  while (Date.now() < deadline) {
    const alerts = await page.locator("[role=alert]").allTextContents();
    const error = alerts.join(" ").match(/\b(?:MAME|PLAYER|PROVIDER|RUNTIME)_[A-Z0-9_]+\b/u)?.[0];
    if (error || await page.getByText("RUNTIME_FAILED", {exact: true}).isVisible()) {await page.screenshot({path: join(directory, `failed-${stage}.png`)}); throw Error(error ?? "MAME_BOOT_FAILED");}
    for (const frame of page.frames()) {
      const canvas = frame.locator('canvas[aria-label="Apple II (MAME)"]');
      if (await canvas.isVisible()) {
        await page.getByRole("status").filter({hasText: "可创建存档"}).waitFor({state: "attached", timeout: 30000});
        const id = launch.launchId ?? launch.previewId;
        const config = await page.evaluate(async value => (await fetch(`/runtime/launches/${value}/config`)).json(), id);
        assert.equal(config.runtime.targetId, "mame-apple2");
        await canvas.click();
        let visible = false;
        for (let attempt = 0; attempt < 45; attempt++) {
          if ((await canvasDigest(canvas)).colors > 2) {visible = true; break;}
          await page.waitForTimeout(1000);
        }
        assert.ok(visible, "MAME_APPLE2_DISK_BOOT_TIMEOUT");
        console.log(`apple2: ${stage} ready`);
        return {page, canvas, config};
      }
    }
    await page.waitForTimeout(100);
  }
  await page.screenshot({path: join(directory, `failed-${stage}.png`)});
  throw Error(`MAME_APPLE2_${stage.toUpperCase()}_TIMEOUT`);
}
