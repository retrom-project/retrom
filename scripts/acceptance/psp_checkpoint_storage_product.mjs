import assert from "node:assert/strict";
import {mkdirSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {createHash} from "node:crypto";
import {gunzipSync} from "node:zlib";
import {createRequire} from "node:module";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, launchCart, gamepad} from "./fantasy_product_client.mjs";
import {waitForPreviewReady, revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/psp-checkpoint-storage");
const sharp = createRequire(new URL("../../web/package.json", import.meta.url))("sharp");
const digest = bytes => createHash("sha256").update(bytes).digest("hex");
const evidence = {caseId: "ACC-SAVE-004", variant: "PSP", status: "FAIL", errors: [], runtimes: []};
mkdirSync(directory, {recursive: true});
let browser, proxy;

async function codecs(page) {
  const realms = await Promise.all(page.frames().map(frame => frame.evaluate(() => window.__checkpointCodecs)));
  return {compress: realms.flatMap(value => value.compress), decompress: realms.flatMap(value => value.decompress)};
}
async function open(context, launch) {
  const page = await context.newPage();
  page.on("pageerror", error => evidence.errors.push(error.message.slice(0, 200)));
  await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded"});
  await waitForPreviewReady(page);
  const frame = page.frames().find(candidate => candidate !== page.mainFrame());
  const config = await page.evaluate(async id => (await fetch(`/runtime/launches/${id}/config`)).json(), launch.launchId);
  assert.equal(config.runtime.targetId, "ppsspp");
  const disk = config.resources.find(resource => resource.kind === "ROM_BLOB");
  assert.equal(disk?.sha256, "0f4ea1d04097c25320366d64dca7c3a03fe44138ba4752647aeea5eed35a087a");
  evidence.runtimes.push({providerVersion: config.runtime.providerVersion, bundleSha256: config.runtime.bundleSha256,
    moduleSha256: config.runtime.moduleSha256});
  return {page, canvas: frame.locator("canvas").first(), config};
}
async function menu(opened, name) {
  for (let attempt = 0; attempt < 30; attempt++) {
    const png = await opened.canvas.screenshot();
    const {data, info} = await sharp(png).resize(480, 272).removeAlpha().raw().toBuffer({resolveWithObject: true});
    // Sky Force's five menu rows have white selected text and grey unselected text.
    const white = Array.from({length: 5}, (_, row) => {
      let count = 0;
      for (let y = Math.floor((0.54 + row * 0.075) * 272); y < (0.60 + row * 0.075) * 272; y++) {
        for (let x = 41; x < 134; x++) {
          const offset = (y * 480 + x) * info.channels;
          if (data[offset] > 210 && data[offset + 1] > 210 && data[offset + 2] > 210) {count++;}
        }
      }
      return count;
    });
    if (Math.max(...white) > 30) {
      writeFileSync(join(directory, name + ".png"), png);
      return {selected: white.indexOf(Math.max(...white)), white};
    }
    await opened.page.waitForTimeout(100);
  }
  throw Error("PSP_MENU_NOT_PRESENTED");
}
async function save(opened, client, launchId) {
  const path = `/api/v1/saves?gameId=${env.RETROM_PSP_GAME_ID}&limit=100`;
  const previous = new Set((await client.json("GET", path)).items.map(item => item.saveStateId));
  await revealPreviewToolbar(opened.page);
  const response = opened.page.waitForResponse(item => item.request().method() === "POST" &&
    new URL(item.url()).pathname === `/runtime/launches/${launchId}/save-states`, {timeout: 60000});
  await opened.page.getByRole("button", {name: "创建存档", exact: true}).click();
  assert.equal((await response).status(), 201);
  const added = (await client.json("GET", path)).items.filter(item => !previous.has(item.saveStateId));
  assert.equal(added.length, 1);
  const screenshot = await client.raw("GET", added[0].screenshotUrl);
  assert.equal(screenshot.status(), 200);
  const png = await screenshot.body();
  const pixels = await sharp(png, {limitInputPixels: 960 * 544}).removeAlpha().raw().toBuffer();
  assert.ok(pixels.some(value => value > 210));
  writeFileSync(join(directory, "uploaded-screenshot.png"), png);
  return added[0];
}

try {
  if (![base, env.RETROM_PSP_GAME_ID, env.RETROM_PSP_SAVE_ID, env.RETROM_CHROME_EXECUTABLE,
    env.RETROM_ACCEPTANCE_USERNAME, env.RETROM_ACCEPTANCE_PASSWORD].every(Boolean)) {
    evidence.status = "BLOCKED"; throw Error("PSP_STORAGE_INPUT_REQUIRED");
  }
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context);
  await context.addInitScript(() => {
    window.__checkpointCodecs = {compress: [], decompress: []};
    for (const [name, key] of [["CompressionStream", "compress"], ["DecompressionStream", "decompress"]]) {
      const Native = window[name];
      window[name] = class extends Native {constructor(format) {super(format); window.__checkpointCodecs[key].push(format);}};
    }
  });
  const client = await fantasyClient(context, base);
  const firstLaunch = await launchCart(client, env.RETROM_PSP_GAME_ID, env.RETROM_PSP_SAVE_ID);
  const first = await open(context, firstLaunch);
  const before = await menu(first, "seed-restored");
  await first.canvas.click(); await gamepad(first.page, 13, 100); await first.page.waitForTimeout(500);
  const moved = await menu(first, "before-save");
  assert.equal(moved.selected, (before.selected + 1) % 5, "PSP_RESTORED_INPUT_FAILED");
  await revealPreviewToolbar(first.page);
  await first.page.getByRole("button", {name: "暂停", exact: true}).click();
  const saved = await save(first, client, firstLaunch.launchId);
  const firstCodecs = await codecs(first.page);
  assert.deepEqual(firstCodecs, {compress: ["gzip"], decompress: ["gzip"]});
  await first.page.close();
  const nextLaunch = await launchCart(client, env.RETROM_PSP_GAME_ID, saved.saveStateId);
  assert.notEqual(firstLaunch.launchId, nextLaunch.launchId);
  const next = await open(context, nextLaunch);
  assert.equal(next.config.restore.format, "emulatorjs-state-v1-storage-v1");
  const response = await client.raw("GET", next.config.restore.url);
  assert.equal(response.status(), 200);
  const stored = await response.body();
  assert.equal(stored.length, saved.sizeBytes); assert.equal(digest(stored), next.config.restore.sha256);
  const native = gunzipSync(stored, {maxOutputLength: 402653184});
  assert.equal(native.subarray(0, 8).toString("hex"), "5241535441544501", "PSP_DOUBLE_COMPRESSION");
  assert.equal(native.subarray(8, 12).toString("ascii"), "MEM ");
  assert.ok(stored.length < native.length / 2);
  const restored = await menu(next, "compressed-restored");
  assert.equal(restored.selected, moved.selected, "PSP_EXECUTION_STATE_NOT_RESTORED");
  await next.canvas.click(); await gamepad(next.page, 12, 100); await next.page.waitForTimeout(500);
  const afterInput = await menu(next, "restored-input");
  assert.equal(afterInput.selected, before.selected);
  const nextCodecs = await codecs(next.page);
  assert.deepEqual(nextCodecs, {compress: [], decompress: ["gzip"]});
  assert.deepEqual(evidence.errors, []);
  Object.assign(evidence, {status: "PASS", gameId: env.RETROM_PSP_GAME_ID, seedSaveId: env.RETROM_PSP_SAVE_ID,
    saveStateId: saved.saveStateId, originalLaunchId: firstLaunch.launchId, restoredLaunchId: nextLaunch.launchId,
    storedBytes: stored.length, nativeBytes: native.length, storedSha256: digest(stored),
    nativeHeaderHex: native.subarray(0, 16).toString("hex"), reductionPercent: (1 - stored.length / native.length) * 100,
    firstCodecs, nextCodecs, menu: {before, moved, restored, afterInput}});
} catch (error) {
  evidence.errorCode = error.message.split("\n")[0].slice(0, 300);
  process.exitCode = evidence.status === "BLOCKED" ? 3 : 1;
} finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "psp-checkpoint-storage-product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}
