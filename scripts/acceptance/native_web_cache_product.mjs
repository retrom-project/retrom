import assert from "node:assert/strict";
import {readFile, mkdir, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {chromium, expect} from "../../web/node_modules/@playwright/test/index.mjs";
import {fantasyClient, launchCart} from "./fantasy_product_client.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {observeContentIO} from "./content_io_observation.mjs";
import {exitContentIOPlayer} from "./content_io_player_exit.mjs";
import {nativeSnapshot, nativeActions, chooseNativeLoading, nativeCacheReceipt, nativeWorkerRestart} from "./native_web_cache_browser.mjs";
import {assertNativePreload, assertNativeLocalResponses, assertNativeInput} from "./native_web_cache_contract.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/native-web-cache");
await mkdir(directory, {recursive: true});
const report = {schemaVersion: 1, caseId: "ACC-CONTENT-002", status: "FAIL", phases: []};
let browser, proxy, context, client, plan;
async function flush() {
  await writeFile(join(directory, "native-web-cache-product.json"), JSON.stringify(report, null, 2) + "\n");
}
try {
  assert.ok(base && env.RETROM_CHROME_EXECUTABLE && env.RETROM_NATIVE_CACHE_INPUT, "NATIVE_CACHE_INPUT_REQUIRED");
  plan = JSON.parse(await readFile(env.RETROM_NATIVE_CACHE_INPUT, "utf8"));
  assert.ok(["rpgmaker-mv", "rpgmaker-mz", "tyranoscript"].includes(plan.targetId), "NATIVE_CACHE_TARGET_INVALID");
  report.targetId = plan.targetId; report.gameId = plan.gameId;
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--autoplay-policy=no-user-gesture-required"]});
  context = await browser.newContext({viewport: {width: 1440, height: 1000}, ...proxy.contextOptions});
  await context.route("**/*", route => route.continue()); // Disable ordinary HTTP caching.
  client = await fantasyClient(context, base);
  await runPhase("PRELOAD"); await runPhase("ON_DEMAND"); report.status = "PASS";
} catch (error) {report.error = error.message; process.exitCode = 1;}
finally {
  await context?.setOffline(false).catch(() => {});
  await browser?.close(); await proxy?.close(); await flush();
  console.log(JSON.stringify({caseId: report.caseId, targetId: report.targetId, status: report.status, error: report.error}));
}

async function runPhase(mode) {
  await chooseNativeLoading(context, base, plan.gameId, mode);
  const launch = await launchCart(client, plan.gameId); launch.returnTo = `/games/${plan.gameId}`;
  const config = await client.json("GET", `/runtime/launches/${launch.launchId}/config`);
  assert.equal(config.runtime.targetId, plan.targetId);
  const resource = config.resources.find(row => row.role === "game");
  const response = await context.request.get(new URL(resource.indexUrl, base).href);
  assert.equal(response.status(), 200); const {files} = await response.json();
  const urls = new Set(files.map(file => new URL(file.url, base).href));
  const network = observeContentIO(context, url => urls.has(url)); await network.ready;
  const phase = {mode, launchId: launch.launchId, expectedBytes: files.reduce((sum, file) => sum + file.sizeBytes, 0),
    expectedFiles: files.length, requests: network.requests, localResponses: [], pageErrors: []};
  report.phases.push(phase); await flush();
  const local = response => {
    if (/^\/__retrom\/(?:tyranoscript\/)?(?:entry$|project\/)|^\/(?:data|tyrano)\//u.test(new URL(response.url()).pathname)) {
      phase.localResponses.push({status: response.status(), local: response.fromServiceWorker()});
    }
  };
  context.on("response", local);
  const page = await context.newPage(); page.on("pageerror", error => phase.pageErrors.push(error.message));
  try {
    await page.goto(`${base}${launch.playUrl}`, {waitUntil: "domcontentloaded"});
    const deadline = Date.now() + 900000;
    let nextProgress = Date.now() + 30000;
    while (!await nativeSnapshot(page)) {
      const failure = (await page.locator(".player-loading").allTextContents()).join(" ");
      assert.ok(!/CONTENT_IO_[A-Z_]+|RUNTIME_[A-Z_]+/u.test(failure), `NATIVE_CACHE_RUNTIME_FAILURE: ${failure}`);
      assert.ok(Date.now() < deadline, "NATIVE_CACHE_BOOT_TIMEOUT");
      if (Date.now() >= nextProgress) {
        console.log(JSON.stringify({targetId: plan.targetId, mode, requests: network.requests.length, loading: failure}));
        await flush(); nextProgress = Date.now() + 30000;
      }
      await page.waitForTimeout(1000);
    }
    await expect(page.locator(".player-loading")).toBeHidden({timeout: 60000});
    phase.booted = await nativeSnapshot(page); await flush();
    await nativeActions(page, plan.start); phase.started = await nativeSnapshot(page); await network.flush();
    if (mode === "PRELOAD") {
      phase.receipt = await nativeCacheReceipt(page, resource.contentDigest);
      assertNativePreload(files, network.requests, phase.receipt);
    } else assert.equal(network.requests.length, 0, "NATIVE_CACHE_WARM_DOWNLOAD");
    await proveOffline(page, phase, files);
    assertNativeLocalResponses(phase.localResponses); assert.equal(phase.pageErrors.length, 0);
    await context.setOffline(false); await exitContentIOPlayer(page, base, launch);
    phase.exited = true;
  } catch (error) {
    phase.failure = {message: error.message, path: new URL(page.url()).pathname,
      frames: page.frames().map(frame => new URL(frame.url()).pathname),
      player: (await page.locator("body").innerText()).slice(-2000)};
    await page.screenshot({path: join(directory, `${mode}-failure.png`)}).catch(() => {});
    throw error;
  } finally {
    await context.setOffline(false); await page.close(); network.close(); context.off("response", local); await flush();
  }
}
async function proveOffline(page, phase, files) {
  const count = phase.requests.length;
  phase.before = await nativeSnapshot(page); await context.setOffline(true);
  await nativeActions(page, plan.advance); phase.after = await nativeSnapshot(page);
  assertNativeInput(phase.before, phase.after);
  const media = files.find(file => /^(?:audio|video)\//u.test(file.mediaType) && file.sizeBytes >= 19);
  assert.ok(media, "NATIVE_CACHE_MEDIA_SAMPLE_REQUIRED");
  phase.workerRestart = await nativeWorkerRestart(context, page, media.path);
  assert.equal(phase.workerRestart.status, 206); assert.equal(phase.workerRestart.bytes, 17);
  assert.equal(phase.requests.length, count, "NATIVE_CACHE_OFFLINE_DOWNLOAD");
  await page.screenshot({path: join(directory, `${phase.mode}-offline.png`)});
}
