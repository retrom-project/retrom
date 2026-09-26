#!/usr/bin/env node
// Focused replay of an operator-provided bookmark, not ACC-KIRIKIRI-001.
import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {mkdirSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {fantasyClient, launchCart, saveCart} from "./fantasy_product_client.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";

const required = name => {assert.ok(process.env[name], `${name}_REQUIRED`); return process.env[name];};
const base = required("RETROM_ACCEPTANCE_BASE_URL");
const gameId = required("RETROM_KIRIKIRI_REPLAY_GAME_ID");
const saveId = required("RETROM_KIRIKIRI_REPLAY_SAVE_ID");
const directory = resolve(required("RETROM_ACCEPTANCE_CASE_DIR"));
const executablePath = required("RETROM_CHROME_EXECUTABLE");
const choiceY = Number(process.env.RETROM_KIRIKIRI_REPLAY_CHOICE_Y ?? "0.33");
assert.ok(choiceY > 0 && choiceY < 1, "REPLAY_CHOICE_Y_INVALID");
mkdirSync(directory, {recursive: true});
const failures = [];
const evidence = {schemaVersion: 1, status: "RUNNING", visualReviewRequired: true, sourceSaveId: saveId, failures};
const proxy = await localRpgAcceptanceProxy(base);
let browser;
const deadline = setTimeout(() => {writeEvidence("TIMEOUT"); process.exit(1);}, 180_000);
try {
  browser = await chromium.launch({executablePath, headless: true});
  evidence.browser = browser.version();
  const context = await browser.newContext({viewport: {width: 1440, height: 1000}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context);
  const client = await fantasyClient(context, base);
  await replay(context, client);
  assert.deepEqual(failures, [], "REPLAY_RUNTIME_ERROR");
  writeEvidence("PASS");
} catch (error) {
  evidence.error = String(error);
  writeEvidence("FAIL");
  process.exitCode = 1;
} finally {
  clearTimeout(deadline);
  await browser?.close();
  await proxy.close();
}

function writeEvidence(status) {
  evidence.status = status;
  writeFileSync(join(directory, "kirikiri-freeze-replay.json"), JSON.stringify(evidence, null, 2));
  process.stdout.write(`kirikiri-freeze-replay: ${status}\n`);
}

async function replay(context, client) {
  const original = await openBookmark(context, client, saveId);
  evidence.originalLaunchId = original.launch.launchId;
  evidence.core = await original.frame.evaluate(async () => ({
    wasmSize: Module.wasmBinary.byteLength,
    wasmSha256: [...new Uint8Array(await crypto.subtle.digest("SHA-256", Module.wasmBinary))]
      .map(value => value.toString(16).padStart(2, "0")).join(""),
  }));
  const before = await capture(original.page, "choice.png");
  await moveToChoice(original.page, original.frame);
  await pressA(original.page, 1, 100);
  await original.page.waitForTimeout(1500);
  assert.notEqual(await capture(original.page, "after-choice.png"), before, "REPLAY_CHOICE_UNCHANGED");
  await original.frame.evaluate(() => {globalThis.__freezeReplayInput = {down: 0, up: 0};
    document.addEventListener("mousedown", () => {__freezeReplayInput.down++;}, true);
    document.addEventListener("mouseup", () => {__freezeReplayInput.up++;}, true);});
  const burstBefore = await capture(original.page, "before-rapid-a.png");
  await pressA(original.page, 400, 20);
  evidence.input = await original.frame.evaluate(() => globalThis.__freezeReplayInput);
  assert.ok(evidence.input.down >= 100, "REPLAY_INSUFFICIENT_GAMEPAD_INPUT");
  assert.equal(evidence.input.down, evidence.input.up, "REPLAY_MOUSE_BUTTON_STUCK");
  assert.notEqual(await capture(original.page, "after-rapid-a.png"), burstBefore, "REPLAY_GAME_STALLED");
  const saved = await saveCart(original.page, original.launch.launchId, "kirikiri2");
  assert.equal(saved.checkpointFormat, "kirikiri-save-bundle-v1-storage-v1");
  evidence.createdSaveId = saved.saveStateId;
  await capture(original.page, "toolbar-after-rapid-a.png");
  await original.page.close();
  const restored = await openBookmark(context, client, saved.saveStateId);
  evidence.restoredLaunchId = restored.launch.launchId;
  assert.notEqual(restored.launch.launchId, original.launch.launchId);
  const restoredBefore = await capture(restored.page, "restored.png");
  await pressA(restored.page, 12, 100);
  assert.notEqual(await capture(restored.page, "after-restored-input.png"), restoredBefore, "REPLAY_RESTORE_STALLED");
  await revealPreviewToolbar(restored.page);
  assert.ok(await restored.page.getByRole("button", {name: "创建存档", exact: true}).isEnabled());
  await restored.page.close();
}

async function openBookmark(context, client, bookmark) {
  const launch = await launchCart(client, gameId, bookmark);
  const page = await context.newPage();
  page.setDefaultTimeout(5000);
  page.on("console", message => {
    if (/致命|Cannot convert the variable type|Aborted\(/u.test(message.text())) {failures.push("CORE_FATAL");}
  });
  page.on("pageerror", () => failures.push("PAGE_ERROR"));
  await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 60_000});
  const canvas = page.frameLocator("iframe").locator("canvas");
  await canvas.waitFor({timeout: 60_000});
  const frame = page.frames().find(value => value !== page.mainFrame());
  // A fresh automation page has no trusted activation from the launch UI.
  // Wait for the core's audio gesture listeners before activating at the edge.
  await frame.waitForFunction(() => typeof AL !== "undefined" &&
    Object.values(AL.contexts ?? {}).some(value => value.audioCtx), null, {timeout: 30_000});
  const box = await canvas.boundingBox();
  await page.mouse.click(box.x + 8, box.y + 100);
  await revealPreviewToolbar(page);
  const button = page.getByRole("button", {name: "创建存档", exact: true});
  const readyDeadline = Date.now() + 60_000;
  while (!await button.isEnabled()) {
    assert.deepEqual(failures, [], "REPLAY_RUNTIME_ERROR");
    assert.ok(Date.now() < readyDeadline, "REPLAY_BOOKMARK_TIMEOUT");
    await page.waitForTimeout(100);
  }
  await page.mouse.move(8, 950);
  await page.waitForTimeout(2200);
  return {page, frame, launch};
}

async function pad(page, index, pressed) {
  await Promise.all(page.frames().map(frame => frame.evaluate(({index, pressed}) => {
    globalThis.__retromTestGamepad.button(index, pressed);
  }, {index, pressed})));
}

async function pressA(page, count, delay) {
  for (let index = 0; index < count; index++) {
    await pad(page, 0, true); await page.waitForTimeout(delay);
    await pad(page, 0, false); await page.waitForTimeout(delay);
    assert.deepEqual(failures, [], "REPLAY_RUNTIME_ERROR");
  }
}

async function moveToChoice(page, frame) {
  for (let attempt = 0; attempt < 50; attempt++) {
    const y = await frame.evaluate(() => {
      const cursor = document.querySelector("[data-gamepad-cursor]")?.getBoundingClientRect();
      const canvas = document.querySelector("canvas").getBoundingClientRect();
      return cursor ? (cursor.y + cursor.height / 2 - canvas.y) / canvas.height : 0.5;
    });
    if (Math.abs(y - choiceY) < 0.015) {return;}
    const button = y > choiceY ? 12 : 13;
    await pad(page, button, true); await page.waitForTimeout(25); await pad(page, button, false);
  }
  throw Error("REPLAY_CURSOR_TARGET_UNREACHED");
}

async function capture(page, name) {
  const bytes = await page.screenshot({path: join(directory, name), timeout: 5000});
  return createHash("sha256").update(bytes).digest("hex");
}
