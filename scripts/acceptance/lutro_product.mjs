import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {existsSync, mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {gunzipSync} from "node:zlib";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import sharp from "../../web/node_modules/sharp/dist/index.cjs";
import {px68kLocalProxy} from "./px68k_product_support.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, approveCart, launchCart, gamepad, saveCart} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";

const env = process.env;
const baseUrl = env.RETROM_ACCEPTANCE_BASE_URL;
const cartPath = resolve(env.RETROM_LUTRO_CART ?? "testdata/public-roms/lutro-smoke/lutro-smoke.lutro");
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/lutro-product");
mkdirSync(directory, {recursive: true});
const progressPath = join(directory, "progress.json");
const evidence = {caseId: "ACC-LUTRO-001", status: "FAIL", stages: [], errors: []};
let browser, proxy;
try {
  assert.ok(baseUrl && env.RETROM_ACCEPTANCE_USERNAME && env.RETROM_ACCEPTANCE_PASSWORD &&
    env.RETROM_CHROME_EXECUTABLE, "LUTRO_ACCEPTANCE_INPUT_REQUIRED");
  const cart = readFileSync(cartPath);
  const digest = createHash("sha256").update(cart).digest("hex");
  proxy = await px68kLocalProxy(baseUrl);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(30000);
  await installVirtualStandardGamepad(context);
  const client = await fantasyClient(context, baseUrl);
  const progress = existsSync(progressPath) ? JSON.parse(readFileSync(progressPath, "utf8")) : {};
  if (progress.digest) {assert.equal(progress.digest, digest, "LUTRO_CART_CHANGED");}
  let {itemId, gameId} = progress;
  if (!itemId) {
    await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
      headers: client.writeHeaders(), data: {}, expected: 200,
    });
    const platforms = await client.json("GET", "/api/v1/admin/platform-instances?platformId=lutro&limit=100");
    const instance = platforms.items.find(item => item.enabled && item.defaultCoreId === "lutro");
    assert.ok(instance, "LUTRO_PLATFORM_MISSING");
    const uploadId = await client.upload(singleFile(cartPath), "FILES", "GENERAL");
    const imported = await client.json("POST", "/api/v1/admin/imports", {
      headers: client.writeHeaders(), expected: 202,
      data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []},
    });
    itemId = (await reviewForImport(client, imported.importJobId)).itemId;
    writeFileSync(progressPath, JSON.stringify({digest, itemId}));
    evidence.stages.push("upload-import-review");
  } else {
    evidence.stages.push("reuse-existing-import-review");
  }
  if (!gameId) {
    const preview = await open(context, await previewCart(client, itemId), "preview");
    evidence.preview = await canvasDigest(preview.canvas);
    assert.ok(evidence.preview.colors > 2, "LUTRO_PREVIEW_EMPTY");
    await preview.canvas.screenshot({path: join(directory, "preview.png")});
    await preview.page.close();
    gameId = (await approveCart(client, itemId)).gameId;
    writeFileSync(progressPath, JSON.stringify({digest, itemId, gameId}));
    evidence.stages.push("preview-publish");
  } else {
    evidence.stages.push("reuse-existing-preview-publish");
  }
  const launch = await launchCart(client, gameId);
  const initial = await open(context, launch, "initial");
  evidence.runtime = initial.config.runtime;
  evidence.initial = await canvasDigest(initial.canvas);
  await gamepad(initial.page, 15, 180);
  evidence.moved = await canvasDigest(initial.canvas);
  assert.notEqual(evidence.initial.sha256, evidence.moved.sha256, "LUTRO_DIRECTION_UNCHANGED");
  await gamepad(initial.page, 1, 180);
  const nativeProgress = await initial.canvas.evaluate(() => {
    const fs = window.EJS_emulator?.gameManager?.FS;
    const file = "/data/saves/lutro/lutro-native/lutro-smoke/progress.txt";
    return fs?.analyzePath(file).exists ? new TextDecoder().decode(fs.readFile(file)) : null;
  });
  assert.equal(nativeProgress, "1", "LUTRO_NATIVE_FILE_NOT_WRITTEN");
  evidence.nativeFileWritten = true;
  await initial.page.getByRole("status").filter({hasText: "已暂存在此浏览器"}).waitFor({state: "attached", timeout: 15000});
  evidence.savedFrame = await canvasDigest(initial.canvas);
  await initial.canvas.screenshot({path: join(directory, "saved.png")});
  const saved = await saveCart(initial.page, launch.launchId, "lutro");
  assert.equal(saved.checkpointFormat, "lutro-native-v1-storage-v1");
  evidence.saveStateId = saved.saveStateId;
  await initial.page.close();
  evidence.stages.push("game-input-native-save");
  const resumedLaunch = await launchCart(client, gameId, saved.saveStateId);
  assert.notEqual(resumedLaunch.launchId, launch.launchId);
  const resumed = await open(context, resumedLaunch, "restore");
  assert.equal(resumed.config.restore?.format, "lutro-native-v1-storage-v1");
  const stored = await client.raw("GET", resumed.config.restore.url);
  assert.equal(stored.status(), 200);
  const packed = await stored.body();
  assert.equal(packed.length, resumed.config.restore.sizeBytes);
  assert.equal(createHash("sha256").update(packed).digest("hex"), resumed.config.restore.sha256);
  const payload = gunzipSync(packed, {maxOutputLength: 16 * 1024 * 1024});
  assert.equal(payload.toString("ascii", 0, 4), "RLNS");
  assert.ok(payload.includes(Buffer.from("progress.txt")) && payload.includes(Buffer.from("1")),
    "LUTRO_NATIVE_PROGRESS_MISSING");
  evidence.checkpointStorage = {packedBytes: packed.length, rawBytes: payload.length};
  evidence.restored = await canvasDigest(resumed.canvas);
  assert.equal(evidence.restored.sha256, evidence.savedFrame.sha256, "LUTRO_PROGRESS_NOT_RESTORED");
  await resumed.canvas.screenshot({path: join(directory, "restored.png")});
  await gamepad(resumed.page, 15, 180);
  evidence.afterRestoreInput = await canvasDigest(resumed.canvas);
  assert.notEqual(evidence.afterRestoreInput.sha256, evidence.restored.sha256, "LUTRO_RESTORED_INPUT_UNCHANGED");
  await resumed.page.close();
  evidence.stages.push("fresh-launch-restore-input");
  const clean = await open(context, await launchCart(client, gameId), "clean");
  evidence.clean = await canvasDigest(clean.canvas);
  assert.equal(evidence.clean.sha256, evidence.initial.sha256, "LUTRO_UNSELECTED_SAVE_LEAKED");
  await clean.page.close();
  evidence.stages.push("unselected-save-clean-launch");
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

async function open(context, launch, stage) {
  const page = await context.newPage();
  page.on("pageerror", error => evidence.errors.push(error.message.slice(0, 200)));
  await page.goto(baseUrl + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
  const deadline = Date.now() + 90000;
  while (Date.now() < deadline) {
    if (page.url().includes("/admin/reviews/") && stage === "preview") {throw Error("LUTRO_PREVIEW_EXITED");}
    let alerts, visibleText;
    try {
      alerts = await page.locator("[role=alert]").allTextContents();
      visibleText = await page.locator("body").innerText();
    } catch (error) {
      if (String(error).includes("Execution context was destroyed")) {continue;}
      throw error;
    }
    const error = (alerts.join(" ") + visibleText).match(/\b(?:LUTRO|PLAYER|PROVIDER|RUNTIME)_[A-Z0-9_]+\b/u)?.[0];
    if (error) {throw Error(error);}
    for (const frame of page.frames()) {
      const canvas = frame.locator("canvas.ejs_canvas");
      if (await canvas.isVisible()) {
        const id = launch.launchId ?? launch.previewId;
        const config = await page.evaluate(async value => (await fetch(`/runtime/launches/${value}/config`)).json(), id);
        assert.equal(config.runtime.targetId, "lutro");
        await canvas.click();
        for (let attempt = 0; attempt < 45; attempt++) {
          if ((await canvasDigest(canvas)).colors > 2) {return {page, canvas, config};}
          await page.waitForTimeout(1000);
        }
      }
    }
    await page.waitForTimeout(100);
  }
  await page.screenshot({path: join(directory, `failed-${stage}.png`)});
  throw Error(`LUTRO_${stage.toUpperCase()}_TIMEOUT`);
}

async function canvasDigest(canvas) {
  const {data, info} = await sharp(await canvas.screenshot()).ensureAlpha().raw().toBuffer({resolveWithObject: true});
  const colors = new Set();
  for (let i = 0; i < data.length; i += 4) {colors.add(`${data[i]},${data[i + 1]},${data[i + 2]}`);}
  return {width: info.width, height: info.height, colors: colors.size,
    sha256: createHash("sha256").update(data).digest("hex")};
}
