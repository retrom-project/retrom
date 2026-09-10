import assert from "node:assert/strict";
import {mkdirSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {fantasyClient, previewCart, approveCart, launchCart, gamepad, saveCart} from "./fantasy_product_client.mjs";

const baseUrl = process.env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/ruffle/product");
mkdirSync(directory, {recursive: true});
const evidence = {schemaVersion: 1, caseId: "ACC-FLASH-001", status: "FAIL", stages: [], errors: []};
const required = ["RETROM_ACCEPTANCE_BASE_URL", "RETROM_ACCEPTANCE_USERNAME", "RETROM_ACCEPTANCE_PASSWORD",
  "RETROM_CHROME_EXECUTABLE", "RETROM_RUFFLE_FIXTURE", "RETROM_RUFFLE_GAME"];
const missing = required.filter((name) => !process.env[name]);
if (missing.length) {
  writeFileSync(join(directory, "ruffle-product.json"), JSON.stringify({...evidence, status: "BLOCKED",
    errorCode: "RUFFLE_ACCEPTANCE_INPUT_REQUIRED", missing}) + "\n");
  process.exit(3);
}
let browser, proxy;
try {
  assert.ok(baseUrl && process.env.RETROM_RUFFLE_FIXTURE && process.env.RETROM_RUFFLE_GAME, "RUFFLE_ACCEPTANCE_INPUT_REQUIRED");
  proxy = await localRpgAcceptanceProxy(baseUrl);
  browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  evidence.browser = browser.version();
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context);
  context.setDefaultTimeout(15000);
  const readiness = await context.request.get(`${baseUrl}/health/ready`);
  assert.equal(readiness.status(), 200, "RUFFLE_PRODUCT_NOT_READY");
  assert.equal((await readiness.json()).status, "ready", "RUFFLE_PRODUCT_NOT_READY");
  const client = await fantasyClient(context, baseUrl);
  const existingGame = process.env.RETROM_RUFFLE_OWNED_GAME_ID;
  const owned = existingGame ? {gameId: existingGame} : await publish(context, client, process.env.RETROM_RUFFLE_FIXTURE, "owned");
  if (existingGame) {evidence.stages.push("owned:reuse-previously-imported-product");}
  await verifySave(context, client, owned.gameId);
  const existingExternal = process.env.RETROM_RUFFLE_EXTERNAL_GAME_ID;
  const external = existingExternal ? {gameId: existingExternal} : await publish(context, client, process.env.RETROM_RUFFLE_GAME, "external");
  if (existingExternal) {evidence.stages.push("external:reuse-previously-imported-product");}
  const launch = await launchCart(client, external.gameId);
  const opened = await open(context, launch, "external-product");
  if (process.env.RETROM_RUFFLE_START_CLICK) {
    const point = process.env.RETROM_RUFFLE_START_CLICK.split(",").map(Number);
    assert.ok(point.length === 2 && point.every((value) => Number.isFinite(value) && value >= 0 && value <= 1), "RUFFLE_START_CLICK_INVALID");
    const bounds = await opened.canvas.boundingBox();
    await opened.canvas.click({position: {x: point[0] * bounds.width, y: point[1] * bounds.height}, delay: 100});
    await opened.page.waitForTimeout(1500);
  }
  if (process.env.RETROM_RUFFLE_START_KEYS) {
    const keys = process.env.RETROM_RUFFLE_START_KEYS;
    assert.match(keys, /^[a-z](?:\+[a-z]){0,3}$/u, "RUFFLE_START_KEYS_INVALID");
    await opened.canvas.press(keys, {delay: 150});
    await opened.page.waitForTimeout(1500);
  }
  await gamepad(opened.page, 0); await gamepad(opened.page, 15);
  await gamepad(opened.page, 2);
  await opened.canvas.screenshot({path: join(directory, "external-after-input.png")});
  await opened.page.close();
  evidence.external = {gameId: external.gameId, launchId: launch.launchId};
  assert.equal(evidence.errors.length, 0, "RUFFLE_BROWSER_ERRORS");
  evidence.status = "PASS";
} catch (error) {
  evidence.errorCode = error.message; process.exitCode = 1;
  for (const [index, page] of (browser?.contexts().flatMap((context) => context.pages()) ?? []).entries()) {
    await page.screenshot({path: join(directory, `failure-${index}.png`)}).catch(() => undefined);
    evidence.failureText = await page.locator("body").innerText().then((text) => text.slice(-2500)).catch(() => "");
  }
} finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "ruffle-product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}

async function publish(context, client, filename, label) {
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  const platforms = await client.json("GET", "/api/v1/admin/platform-instances?platformId=flash&limit=100");
  const instance = platforms.items.find((item) => item.enabled && item.defaultCoreId === "ruffle");
  assert.ok(instance, "RUFFLE_PLATFORM_MISSING");
  const uploadId = await client.upload(singleFile(filename), "FILES", "GENERAL");
  const imported = await client.json("POST", "/api/v1/admin/imports", {
    headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []},
  });
  const review = await reviewForImport(client, imported.importJobId);
  const preview = await previewCart(client, review.itemId);
  const opened = await open(context, preview, `${label}-preview`);
  await gamepad(opened.page, 0); await opened.page.close();
  const published = await approveCart(client, review.itemId);
  evidence.stages.push(`${label}:upload-review-preview-publish`);
  return published;
}

async function open(context, launch, label) {
  const page = await context.newPage();
  page.on("pageerror", (error) => evidence.errors.push(error.message.slice(0, 250)));
  page.on("console", (message) => {
    if (message.type() === "error") {evidence.lastConsoleError = message.text().slice(0, 400);}
  });
  await page.goto(`${baseUrl}${launch.playUrl}`, {waitUntil: "domcontentloaded", timeout: 60000});
  for (const deadline = Date.now() + 60000; Date.now() < deadline;) {
    for (const frame of page.frames()) {
      const canvas = frame.locator("ruffle-player canvas").first();
      if (await canvas.isVisible()) {
        await frame.waitForFunction(() => document.querySelector("ruffle-player")?.ruffle().readyState === 2, null, {timeout: 45000});
        await page.waitForTimeout(1200);
        await page.screenshot({path: join(directory, `${label}-surface.png`)});
        const accelerationNotice = frame.locator("#hardware-acceleration-modal .close-modal");
        if (await accelerationNotice.isVisible()) {
          evidence.softwareRendering = true;
          await frame.locator("#hardware-acceleration-modal").click({position: {x: 10, y: 100}});
        }
        await canvas.click({timeout: 5000}).catch(async (error) => {
          if (!await accelerationNotice.isVisible()) {throw error;}
          evidence.softwareRendering = true;
          await frame.locator("#hardware-acceleration-modal").click({position: {x: 10, y: 100}});
          await canvas.click();
        });
        await page.waitForTimeout(800);
        await canvas.screenshot({path: join(directory, `${label}.png`)});
        return {page, canvas};
      }
    }
    const alerts = await page.getByRole("alert").allTextContents();
    if (alerts.some((text) => /RUFFLE_|PROVIDER_|RUNTIME_FAILED/u.test(text))) {throw Error("RUFFLE_RUNTIME_FAILED:" + alerts.join(" "));}
    await page.waitForTimeout(100);
  }
  await page.screenshot({path: join(directory, `${label}-failed.png`)});
  throw Error("RUFFLE_CANVAS_TIMEOUT");
}

async function verifySave(context, client, gameId) {
  const original = await launchCart(client, gameId);
  const first = await open(context, original, "owned-start");
  const initial = await position(first.canvas); assert.ok(Math.abs(initial - 20) < 2, "RUFFLE_INITIAL_POSITION");
  await gamepad(first.page, 15);
  const moved = await position(first.canvas); assert.ok(moved > initial + 10, "RUFFLE_DIRECTION_FAILED");
  await gamepad(first.page, 1); assert.ok(Math.abs(await position(first.canvas) - initial) < 2, "RUFFLE_CANCEL_FAILED");
  await gamepad(first.page, 15); await gamepad(first.page, 0);
  const savedPosition = await position(first.canvas);
  await first.page.waitForTimeout(1500);
  // This helper selects the existing GAME_SAVE exit flow, also used by TIC-80.
  const saved = await saveCart(first.page, original.launchId, "tic80");
  assert.equal(saved.checkpointFormat, "ruffle-sharedobjects-v1-storage-v1");
  await first.page.close();
  const restored = await launchCart(client, gameId, saved.saveStateId);
  assert.notEqual(restored.launchId, original.launchId);
  const second = await open(context, restored, "owned-restored");
  assert.ok(Math.abs(await position(second.canvas) - savedPosition) < 2, "RUFFLE_RESTORE_FAILED");
  await gamepad(second.page, 14);
  assert.ok(await position(second.canvas) < savedPosition - 10, "RUFFLE_RESTORED_INPUT_FAILED");
  await gamepad(second.page, 15); await gamepad(second.page, 15); await gamepad(second.page, 0);
  const updatedPosition = await position(second.canvas);
  const updated = await saveCart(second.page, restored.launchId, "tic80");
  assert.equal(updated.saveStateId, saved.saveStateId, "RUFFLE_RESTORE_CREATED_ANOTHER_CONTAINER");
  await second.page.close();
  const updatedLaunch = await open(context, await launchCart(client, gameId, saved.saveStateId), "owned-updated-container");
  assert.ok(Math.abs(await position(updatedLaunch.canvas) - updatedPosition) < 2, "RUFFLE_CONTAINER_UPDATE_NOT_RESTORED");
  await updatedLaunch.page.close();
  const fresh = await launchCart(client, gameId);
  const clean = await open(context, fresh, "owned-clean-launch");
  assert.ok(Math.abs(await position(clean.canvas) - initial) < 2, "RUFFLE_UNREQUESTED_RESTORE");
  await gamepad(clean.page, 15); await gamepad(clean.page, 0);
  const newContainer = await saveCart(clean.page, fresh.launchId, "tic80");
  assert.notEqual(newContainer.saveStateId, saved.saveStateId, "RUFFLE_NEW_GAME_REUSED_CONTAINER");
  await clean.page.close();
  evidence.save = {gameId, originalLaunchId: original.launchId, restoredLaunchId: restored.launchId,
    saveStateId: saved.saveStateId, updatedSaveStateId: updated.saveStateId, newSaveStateId: newContainer.saveStateId,
    checkpointFormat: saved.checkpointFormat, initial, savedPosition, updatedPosition};
  evidence.stages.push("owned:direction-confirm-cancel-native-save-transfer-fresh-launch-restore-input-clean-launch");
}

async function position(canvas) {
  const png = await canvas.evaluate(async (element) => {
    const blob = await element.getRootNode().host.ruffle().captureFrame();
    const bytes = new Uint8Array(await blob.arrayBuffer());
    let encoded = "";
    for (let i = 0; i < bytes.length; i += 8192) {encoded += String.fromCharCode(...bytes.subarray(i, i + 8192));}
    return btoa(encoded);
  });
  return canvas.page().evaluate(async (encoded) => {
    const bytes = Uint8Array.from(atob(encoded), (c) => c.charCodeAt(0));
    const image = await createImageBitmap(new Blob([bytes], {type: "image/png"}));
    const surface = document.createElement("canvas"); surface.width = image.width; surface.height = image.height;
    const ctx = surface.getContext("2d"); ctx.drawImage(image, 0, 0); image.close();
    const scale = Math.min(surface.width / 320, surface.height / 240);
    const left = (surface.width - scale * 320) / 2;
    const top = (surface.height - scale * 240) / 2;
    const row = ctx.getImageData(0, Math.floor(top + scale * 105), surface.width, 1).data;
    for (let x = 0; x < surface.width; x++) {
      if (row[x * 4] > 230 && row[x * 4 + 1] > 230 && row[x * 4 + 2] > 230) {return (x - left) / scale;}
    }
    throw Error("RUFFLE_FIXTURE_MARKER_MISSING");
  }, png);
}
