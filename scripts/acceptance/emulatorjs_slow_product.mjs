import assert from "node:assert/strict";
import {mkdir, readFile, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {chromium, expect} from "../../web/node_modules/@playwright/test/index.mjs";
import {fantasyClient, launchCart, saveCart} from "./fantasy_product_client.mjs";
import {emulatorjsSlowProxy} from "./emulatorjs_slow_proxy.mjs";
import {exitContentIOPlayer} from "./content_io_player_exit.mjs";
import {verifyEmulatorJsStartupFailures} from "./emulatorjs_startup_failures.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/emulatorjs-slow");
await mkdir(directory, {recursive: true});
const screenshots = join(directory, "screenshots"); await mkdir(screenshots, {recursive: true});
const report = {schemaVersion: 1, caseId: "ACC-RUN-019", status: "FAIL", bytesPerSecond: 131072, latencyMs: 300, targets: []};
let browser;
const flush = () => writeFile(join(directory, "emulatorjs-slow-product.json"), JSON.stringify(report, null, 2) + "\n");
try {
  assert.ok(base && env.RETROM_EJS_SLOW_INPUT && env.RETROM_CHROME_EXECUTABLE, "EJS_SLOW_INPUT_REQUIRED");
  const input = JSON.parse(await readFile(env.RETROM_EJS_SLOW_INPUT, "utf8"));
  assert.deepEqual(Object.keys(input).sort(), ["fbneo", "mame2003"]);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--autoplay-policy=no-user-gesture-required"]});
  for (const core of ["fbneo", "mame2003"]) {await verify(core, input[core]);}
  Object.assign(report, await verifyEmulatorJsStartupFailures({browser, base, input: input.fbneo, screenshots}));
  report.status = "PASS";
} catch (error) {report.error = error.message; process.exitCode = 1;}
finally {await browser?.close(); await flush(); console.log(JSON.stringify({status: report.status, error: report.error}));}

async function verify(core, input) {
  assert.equal(input.fixtureId, core); assert.equal(input.coreId, core);
  const proxy = await emulatorjsSlowProxy(base), context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  const result = {core, gameId: input.gameId, requests: proxy.requests, contentRequests: proxy.contentRequests}; report.targets.push(result);
  try {
    const client = await fantasyClient(context, base);
    const launch = await launchCart(client, input.gameId); launch.returnTo = `/games/${input.gameId}`;
    const page = await context.newPage(), errors = []; page.on("pageerror", error => errors.push(error.message));
    const configResponse = page.waitForResponse(response => response.url().endsWith(`/runtime/launches/${launch.launchId}/config`));
    const started = performance.now(); await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded"});
    const config = await (await configResponse).json();
    assert.equal(config.runtime.providerId, "emulatorjs"); assert.equal(config.runtime.targetId, core);
    result.runtime = {moduleSha256: config.runtime.moduleSha256, bundleSha256: config.runtime.bundleSha256};
    await expect(page.locator(".player-loading")).toBeHidden({timeout: 150000});
    result.coldReadyMs = performance.now() - started;
    assert.ok(result.coldReadyMs > 30000, "EJS_SLOW_DID_NOT_EXERCISE_DEADLINE");
    assert.ok(proxy.requests.some(row => row.complete && row.elapsedMs > 30000 && row.bytes > 4 * 1048576));
    const canvas = page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas");
    await expect(canvas).toBeVisible(); await canvas.click();
    await canvas.press("Enter"); await canvas.press("ArrowRight", {delay: 200});
    const before = await checkpoint(page); await canvas.press("ArrowLeft", {delay: 200});
    const after = await checkpoint(page); assert.notEqual(before.sha256, after.sha256, "EJS_SLOW_INPUT_DID_NOT_ADVANCE_STATE");
    result.input = {before, after};
    const saved = await saveCart(page, launch.launchId, core); result.saveStateId = saved.saveStateId;
    await page.screenshot({path: join(screenshots, `${core}-cold.png`)});
    await exitContentIOPlayer(page, base, launch);
    // Parent is managed by Content I/O for these targets. Their game/BIOS
    // remain upstream loader inputs, and core transport stays slow.
    const parentRequests = () => proxy.contentRequests.filter(row => row.path.startsWith("/runtime/content/parent/"));
    const parentRequestCount = parentRequests().length; proxy.blockContent(["parent"]);
    const networkSession = await context.newCDPSession(page);
    await networkSession.send("Network.enable");
    await networkSession.send("Network.setCacheDisabled", {cacheDisabled: true});
    const restore = await launchCart(client, input.gameId, saved.saveStateId); restore.returnTo = launch.returnTo;
    const warm = performance.now(); await page.goto(base + restore.playUrl, {waitUntil: "domcontentloaded"});
    await expect(page.locator(".player-loading")).toBeHidden({timeout: 150000});
    result.warmReadyMs = performance.now() - warm;
    assert.equal(parentRequests().length, parentRequestCount, "EJS_WARM_PARENT_REQUESTED");
    result.restore = await checkpoint(page); assert.ok(result.restore.sizeBytes > 0);
    await page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas").press("ArrowRight", {delay: 200});
    result.restoreInput = await checkpoint(page); assert.notEqual(result.restore.sha256, result.restoreInput.sha256);
    await context.setOffline(true);
    await page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas").press("ArrowLeft", {delay: 200});
    result.offlineInput = await checkpoint(page); assert.notEqual(result.offlineInput.sha256, result.restoreInput.sha256);
    await context.setOffline(false); await networkSession.detach();
    assert.deepEqual(errors, []); await exitContentIOPlayer(page, base, restore);
    result.status = "PASS";
  } finally {await context.close(); await proxy.close(); await flush();}
}

function checkpoint(page) {
  return page.evaluate(async () => {
    const runtime = window.__RETROM_E2E_RUNTIME_V1__;
    if (!runtime || !["RUNNING", "PAUSED"].includes(runtime.getState())) {throw Error("EJS_SLOW_RUNTIME_NOT_READY");}
    return runtime.checkpoint();
  });
}
