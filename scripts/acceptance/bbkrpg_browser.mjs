import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {writeFileSync} from "node:fs";
import {join} from "node:path";
import sharp from "../../web/node_modules/sharp/dist/index.mjs";
import {gamepad} from "./fantasy_product_client.mjs";
import {resumePreview, revealPreviewToolbar, waitForPreviewReady} from "./rpgmaker_preview_actions.mjs";

export const hash = bytes => createHash("sha256").update(bytes).digest("hex");

export async function openBBKRPG(context, base, launch, evidence, directory) {
  const page = await context.newPage();
  page.on("pageerror", error => evidence.errors.push(error.message.slice(0, 200)));
  page.on("console", message => {
    if (["error", "warning"].includes(message.type())) {
      writeFileSync(join(directory, "bbkrpg-browser-diagnostics.log"), message.text() + "\n", {flag: "a"});
    }
  });
  await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
  try {await waitForPreviewReady(page);} catch (error) {
    await page.screenshot({path: join(directory, "screenshots", "startup-failure.png")});
    throw error;
  }
  const frame = page.frames().find(candidate => candidate !== page.mainFrame());
  assert.ok(frame, "BBKRPG_NATIVE_FRAME_MISSING");
  const canvas = frame.locator("canvas.ejs_canvas");
  await canvas.waitFor({state: "visible", timeout: 30000});
  const id = launch.launchId ?? launch.previewId;
  const config = await page.evaluate(async identifier =>
    (await fetch("/runtime/launches/" + identifier + "/config")).json(), id);
  assert.equal(config.runtime.providerId, "emulatorjs");
  assert.equal(config.runtime.targetId, "gam4980");
  assert.equal(config.runtime.checkpoint.writeFormat, "gam4980-state-v1-storage-v1");
  assert.equal(config.runtime.capabilities.standardGamepad, true);
  assert.equal(config.runtime.capabilities.volume, false);
  const cart = config.resources.find(resource => resource.kind === "ROM_BLOB");
  assert.equal(cart.sha256, evidence.cart.sha256);
  assert.equal(cart.sizeBytes, evidence.cart.sizeBytes);
  const coreSha256 = await page.evaluate(async root => {
    const response = await fetch(root + "assets/4.2.3/data/cores/gam4980-wasm.data");
    if (!response.ok) {throw Error("BBKRPG_CORE_FETCH_FAILED");}
    return [...new Uint8Array(await crypto.subtle.digest("SHA-256", await response.arrayBuffer()))]
      .map(value => value.toString(16).padStart(2, "0")).join("");
  }, config.runtime.runtimeBaseUrl);
  assert.equal(coreSha256, process.env.RETROM_BBKRPG_CORE_SHA256);
  evidence.runtimes.push({id, coreSha256, providerId: config.runtime.providerId,
    targetId: config.runtime.targetId, providerVersion: config.runtime.providerVersion,
    bundleSha256: config.runtime.bundleSha256, moduleSha256: config.runtime.moduleSha256});
  await canvas.click();
  return {page, frame, canvas, config};
}

export async function pictureBBKRPG(opened, directory, name) {
  const png = await opened.canvas.screenshot();
  if (name) {writeFileSync(join(directory, "screenshots", name + ".png"), png);}
  const metadata = await sharp(png).metadata();
  const scale = Math.min(metadata.width / 159, metadata.height / 96);
  const width = Math.floor(159 * scale), height = Math.floor(96 * scale);
  const viewport = {width, height, left: Math.floor((metadata.width - width) / 2),
    top: Math.floor((metadata.height - height) / 2)};
  const {data, info} = await sharp(png).extract(viewport).resize(159, 96, {fit: "fill", kernel: "nearest"})
    .removeAlpha().raw().toBuffer({resolveWithObject: true});
  const bits = Buffer.alloc(159 * 96);
  for (let i = 0; i < bits.length; i++) {
    bits[i] = data[i * info.channels] + data[i * info.channels + 1] + data[i * info.channels + 2] < 384 ? 1 : 0;
  }
  const count = (left, top, right, bottom) => {
    let result = 0;
    for (let y = top; y < bottom; y++) {
      for (let x = left; x < right; x++) {result += bits[y * 159 + x];}
    }
    return result;
  };
  const upperArrow = count(111, 34, 127, 45), lowerArrow = count(111, 48, 127, 60);
  const menuBorder = count(23, 25, 136, 29);
  const darkPixels = bits.reduce((sum, value) => sum + value, 0);
  const blank = darkPixels <= 40 || darkPixels >= 14500;
  const selected = blank ? null : menuBorder > 110 && upperArrow > 8 && lowerArrow < 5 ? "new"
    : menuBorder > 110 && lowerArrow > 8 && upperArrow < 5 ? "load" : null;
  return {sha256: hash(bits), selected, upperArrow, lowerArrow, menuBorder, darkPixels, blank, viewport};
}

export async function pressBBKRPG(opened, button) {
  await resumePreview(opened.page);
  await opened.canvas.click();
  await gamepad(opened.page, button, 150);
  await opened.page.waitForTimeout(700);
}

export async function keyboardBBKRPG(opened, key) {
  await resumePreview(opened.page);
  await opened.canvas.click();
  // Retrom's keyboard mapping is sampled by the native gamepad poll.
  // A keydown/keyup pair in a single frame can disappear before that poll.
  await opened.canvas.press(key, {delay: 150});
  await opened.page.waitForTimeout(700);
}

export async function mainMenuBBKRPG(opened, directory) {
  await opened.page.waitForTimeout(1500);
  for (let attempt = 0; attempt < 40; attempt++) {
    const current = await pictureBBKRPG(opened, directory, "menu-probe-" + attempt);
    if (current.selected) {return current;}
    if (!current.blank && attempt % 6 === 0) {await pressBBKRPG(opened, 0);}
    else {await opened.page.waitForTimeout(500);}
  }
  throw Error("BBKRPG_TITLE_CONFIRM_FAILED");
}

export async function saveBBKRPG(opened, client, launchId, gameId, directory) {
  await opened.frame.evaluate(() => {
    const manager = window.EJS_emulator.gameManager;
    for (const name of ["getState", "getStateAsync"]) {
      const original = manager[name];
      if (!original) {continue;}
      const observe = value => {
        const copy = new Uint8Array(value.buffer, value.byteOffset, value.byteLength).slice();
        window.__bbkrpgSavedNative = crypto.subtle.digest("SHA-256", copy).then(digest => ({
          sizeBytes: copy.length,
          sha256: [...new Uint8Array(digest)].map(byte => byte.toString(16).padStart(2, "0")).join(""),
        }));
        return value;
      };
      manager[name] = function(...args) {
        const value = Reflect.apply(original, this, args);
        return value?.then ? value.then(observe) : observe(value);
      };
    }
  });
  const path = "/api/v1/saves?gameId=" + gameId + "&limit=100";
  const previous = new Set((await client.json("GET", path)).items.map(item => item.saveStateId));
  await revealPreviewToolbar(opened.page);
  const pause = opened.page.getByRole("button", {name: "暂停", exact: true});
  if (await pause.isVisible()) {await pause.click();}
  const before = await pictureBBKRPG(opened, directory, "B-before-save");
  await revealPreviewToolbar(opened.page);
  const response = opened.page.waitForResponse(item => item.request().method() === "POST" &&
    new URL(item.url()).pathname === "/runtime/launches/" + launchId + "/save-states", {timeout: 60000});
  await opened.page.getByRole("button", {name: "创建存档", exact: true}).click();
  assert.equal((await response).status(), 201, "BBKRPG_SAVE_HTTP_FAILED");
  const added = (await client.json("GET", path)).items.filter(item => !previous.has(item.saveStateId));
  assert.equal(added.length, 1, "BBKRPG_SAVE_RECEIPT_AMBIGUOUS");
  const screenshot = await client.raw("GET", added[0].screenshotUrl);
  assert.equal(screenshot.status(), 200);
  writeFileSync(join(directory, "screenshots", "uploaded-screenshot.png"), await screenshot.body());
  return {saved: added[0], before, native: await opened.frame.evaluate(() => window.__bbkrpgSavedNative)};
}
