import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {mkdirSync, readFileSync, writeFileSync, existsSync} from "node:fs";
import {join, resolve} from "node:path";
import {gunzipSync} from "node:zlib";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {px68kLocalProxy, canvasDigest} from "./px68k_product_support.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, approveCart, launchCart, gamepad, saveCart} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";

const env = process.env;
const baseUrl = env.RETROM_ACCEPTANCE_BASE_URL;
const biosDirectory = env.RETROM_APPLE2_BIOS_DIR;
const diskPath = env.RETROM_APPLE2_DISK;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/apple2-product");
mkdirSync(directory, {recursive: true});
const evidence = {caseId: "ACC-APPLE2-001", status: "FAIL", stages: [], errors: []};
const progressPath = join(directory, "progress.json");
let browser, proxy;
try {
  assert.ok(baseUrl && biosDirectory && diskPath && env.RETROM_CHROME_EXECUTABLE, "APPLE2_ACCEPTANCE_INPUT_REQUIRED");
  proxy = await px68kLocalProxy(baseUrl);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(30000);
  await installVirtualStandardGamepad(context);
  const client = await fantasyClient(context, baseUrl);
  const catalog = await client.json("GET", "/api/v1/admin/bios?scope=FULL_CATALOG&coreId=apple2js&limit=100");
  const requirements = catalog.items.filter(item => item.coreId === "apple2js");
  assert.equal(requirements.length, 3, "APPLE2_BIOS_CATALOG_MISSING");
  for (const requirement of requirements) {
    if (requirement.status === "MATCHED") {continue;}
    assert.equal(requirement.activeInstallation, null, "APPLE2_BIOS_INSTALLATION_CONFLICT");
    const uploadId = await client.upload(singleFile(join(biosDirectory, requirement.logicalName)), "FILES", "GENERAL");
    const upload = await client.json("GET", `/api/v1/admin/uploads/${uploadId}`);
    const result = await client.raw("POST", `/api/v1/admin/bios/${requirement.id}/installations`, {
      headers: {...client.writeHeaders(), "If-Match": `"v${requirement.version}"`},
      data: {uploadFileId: upload.files[0].fileId},
    });
    assert.equal(result.status(), 201, `APPLE2_BIOS_INSTALL_FAILED:${requirement.logicalName}`);
  }
  evidence.stages.push("bios"); console.log("apple2: bios");
  const disk = readFileSync(diskPath);
  const digest = createHash("sha256").update(disk).digest("hex");
  const progress = existsSync(progressPath) ? JSON.parse(readFileSync(progressPath, "utf8")) : {};
  if (progress.digest) {assert.equal(progress.digest, digest, "APPLE2_ACCEPTANCE_DISK_CHANGED");}
  let {itemId, gameId} = progress;
  if (!itemId) {
    await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
      headers: client.writeHeaders(), data: {}, expected: 200,
    });
    const instances = await client.json("GET", "/api/v1/admin/platform-instances?platformId=apple2&limit=100");
    const instance = instances.items.find(item => item.enabled && item.defaultCoreId === "apple2js");
    assert.ok(instance, "APPLE2_PLATFORM_MISSING");
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
    assert.ok(evidence.preview.colors > 2, "APPLE2_PREVIEW_EMPTY");
    await preview.page.close();
    gameId = (await approveCart(client, itemId)).gameId;
    writeFileSync(progressPath, JSON.stringify({digest, itemId, gameId}));
  }
  evidence.stages.push("review-preview-publish"); console.log("apple2: publish");
  const launch = await launchCart(client, gameId);
  const opened = await open(context, launch, "product");
  evidence.runtime = opened.config.runtime;
  await opened.page.waitForTimeout(16000);
  await opened.canvas.screenshot({path: join(directory, "before-input.png")});
  evidence.menu = await canvasDigest(opened.canvas);
  await gamepad(opened.page, 13, 700);
  await opened.page.waitForTimeout(500);
  await opened.canvas.screenshot({path: join(directory, "after-direction.png")});
  evidence.menuAfterDirection = await canvasDigest(opened.canvas);
  await gamepad(opened.page, 0, 300);
  await opened.page.waitForTimeout(2500);
  await opened.canvas.screenshot({path: join(directory, "after-confirm.png")});
  evidence.afterConfirm = await canvasDigest(opened.canvas);
  assert.notEqual(evidence.menu.sha256, evidence.menuAfterDirection.sha256, "APPLE2_DIRECTION_SCREEN_UNCHANGED");
  assert.notEqual(evidence.menuAfterDirection.sha256, evidence.afterConfirm.sha256, "APPLE2_CONFIRM_SCREEN_UNCHANGED");
  await opened.canvas.screenshot({path: join(directory, "after-input.png")});
  evidence.before = await canvasDigest(opened.canvas);
  evidence.beforeLightFraction = await lightFraction(opened.canvas);
  const saved = await saveCart(opened.page, launch.launchId, "apple2js");
  assert.equal(saved.checkpointFormat, "apple2js-state-v1-storage-v1");
  evidence.saveStateId = saved.saveStateId;
  await opened.page.close();
  evidence.stages.push("product-input-save"); console.log("apple2: save");
  const resumedLaunch = await launchCart(client, gameId, saved.saveStateId);
  assert.notEqual(resumedLaunch.launchId, launch.launchId);
  const resumed = await open(context, resumedLaunch, "restore");
  assert.equal(resumed.config.restore?.format, "apple2js-state-v1-storage-v1");
  const stored = await client.raw("GET", resumed.config.restore.url);
  assert.equal(stored.status(), 200, "APPLE2_SAVE_NOT_READABLE");
  const packed = await stored.body();
  assert.equal(packed.length, resumed.config.restore.sizeBytes);
  assert.equal(createHash("sha256").update(packed).digest("hex"), resumed.config.restore.sha256);
  const rawState = gunzipSync(packed, {maxOutputLength: 32 * 1024 * 1024});
  assert.equal(rawState[0], 0x7b, "APPLE2_SAVE_HAS_EXTRA_COMPRESSION_LAYER");
  evidence.checkpointStorage = {packedBytes: packed.length, rawBytes: rawState.length};
  evidence.restoredBeforeInput = await canvasDigest(resumed.canvas);
  await gamepad(resumed.page, 14, 500);
  await resumed.canvas.screenshot({path: join(directory, "restored.png")});
  evidence.restored = await canvasDigest(resumed.canvas);
  assert.notEqual(evidence.restoredBeforeInput.sha256, evidence.restored.sha256, "APPLE2_RESTORED_DIRECTION_SCREEN_UNCHANGED");
  evidence.restoredLightFraction = await lightFraction(resumed.canvas);
  assert.ok(Math.abs(evidence.restoredLightFraction - evidence.beforeLightFraction) < 0.15,
    "APPLE2_RESTORE_VISUAL_CORRUPTION");
  await resumed.page.close();
  evidence.stages.push("fresh-launch-restore-input");
  if (env.RETROM_APPLE2_LEGACY_SAVE_ID) {
    const oldLaunch = await launchCart(client, gameId, env.RETROM_APPLE2_LEGACY_SAVE_ID);
    const old = await open(context, oldLaunch, "legacy-restore");
    assert.equal(old.config.restore?.format, "apple2js-state-gzip-v1-storage-v1");
    evidence.legacyRestore = await canvasDigest(old.canvas);
    await old.canvas.screenshot({path: join(directory, "legacy-restored.png")});
    await old.page.close();
    evidence.stages.push("legacy-checkpoint-restore");
  }
  assert.deepEqual(evidence.errors, []);
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
  page.on("pageerror", error => evidence.errors.push(error.message.slice(0, 200)));
  await page.goto(baseUrl + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
  const deadline = Date.now() + 90000;
  while (Date.now() < deadline) {
    const alerts = await page.locator("[role=alert]").allTextContents();
    const error = alerts.join(" ").match(/\b(?:APPLE2JS|PLAYER|PROVIDER|RUNTIME)_[A-Z0-9_]+\b/u)?.[0];
    if (error) {throw Error(error);}
    for (const frame of page.frames()) {
      const canvas = frame.locator("canvas#screen");
      if (await canvas.isVisible()) {
        await page.getByRole("status").filter({hasText: "可创建存档"}).waitFor({state: "attached", timeout: 30000});
        const id = launch.launchId ?? launch.previewId;
        const config = await page.evaluate(async value => (await fetch(`/runtime/launches/${value}/config`)).json(), id);
        assert.equal(config.runtime.targetId, "apple2-apple2js");
        await canvas.click();
        let visible = false;
        for (let attempt = 0; attempt < 45; attempt++) {
          if ((await canvasDigest(canvas)).colors > 2) {visible = true; break;}
          await page.waitForTimeout(1000);
        }
        assert.ok(visible, "APPLE2_DISK_BOOT_TIMEOUT");
        console.log(`apple2: ${stage} ready`);
        return {page, canvas, config};
      }
    }
    await page.waitForTimeout(100);
  }
  await page.screenshot({path: join(directory, `failed-${stage}.png`)});
  throw Error(`APPLE2_${stage.toUpperCase()}_TIMEOUT`);
}
