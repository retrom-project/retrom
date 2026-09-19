import assert from "node:assert/strict";
import {sourceReceiptDigest} from "./content_io_case_proof.mjs";
import {createHash, randomUUID} from "node:crypto";
import {mkdir, readFile, writeFile} from "node:fs/promises";
import {resolve, join} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {fantasyClient, approveCart, importCart} from "./fantasy_product_client.mjs";
import {fantasyRunCart} from "./fantasy_run_cart.mjs";
import {fantasyPerformanceAssets} from "./fantasy_performance_assets.mjs";
import {compareContentIOPerformance} from "./content_io_performance.mjs";
import {measureFantasyLaunch, fantasyObservation} from "./fantasy_performance_browser.mjs";

const sha = bytes => createHash("sha256").update(bytes).digest("hex");
const core = process.argv[2]; assert.ok(["tic80", "fake08"].includes(core));
const caseId = core === "tic80" ? "ACC-TIC-001" : "ACC-PICO-001";
const env = process.env, directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR);
const spec = JSON.parse(await readFile(env.RETROM_CONTENT_IO_PERFORMANCE_INPUT, "utf8"));
await mkdir(directory, {recursive: false});
const report = {schemaVersion: 1, caseId, runId: env.RETROM_CONTENT_IO_RUN_ID ?? null, status: "FAIL", observationId: fantasyObservation(core), samples: [], warmup: []};
let browser, proxy;
const watchdog = setTimeout(() => {void finish(new Error("CONTENT_IO_PERFORMANCE_TIMEOUT")).finally(() => process.exit(1));}, 300000);
async function finish(error) {
  clearTimeout(watchdog);
  if (error) {report.errorCode = error.message; report.stack = error.stack; process.exitCode = 1;}
  await browser?.close(); await proxy?.close();
  await writeFile(join(directory, "performance.json"), JSON.stringify(report, null, 2) + "\n");
}
async function open(variant) {
  proxy = await localRpgAcceptanceProxy(spec[variant].baseURL);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true, args: spec.chromeArgs});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context);
  const collector = await observeContentStoreEvents(context, {retain: true});
  const client = await fantasyClient(context, spec[variant].baseURL);
  return {context, collector, client};
}
async function close() {await browser.close(); browser = null; await proxy.close(); proxy = null;}
async function publish(client, filename) {return approveCart(client, (await importCart(client, core, filename)).itemId);}

try {
  assert.ok(env.RETROM_CHROME_EXECUTABLE && env.RETROM_ACCEPTANCE_USERNAME && env.RETROM_ACCEPTANCE_PASSWORD);
  assert.notEqual(spec.baseline.baseURL, spec.candidate.baseURL);
  assert.notEqual(spec.baseline.bundleSha256, spec.candidate.bundleSha256);
  assert.deepEqual(spec.chromeArgs, ["--use-angle=swiftshader", "--enable-unsafe-swiftshader"]);
  const assets = {baseline: await fantasyPerformanceAssets(spec.baseline, core), candidate: await fantasyPerformanceAssets(spec.candidate, core)};
  assert.deepEqual(assets.baseline, assets.candidate, "FANTASY_PERFORMANCE_CORE_CHANGED"); report.assets = assets;
  const extension = core === "tic80" ? "tic" : "p8";
  const fixture = await readFile(new URL(`../../testdata/public-roms/fantasy-controls/controls.${extension}`, import.meta.url));
  assert.equal(sha(fixture), core === "tic80" ? "c637c1f3e6f24af56850448fcd6fd6e6c06fa4e40fd735c02582b2cff20fb029" : "9965557a27b806c95174d5aabedb97d166836c26d4ac3372ae2b6bd44c6558f3");
  const bytes = fantasyRunCart(core, fixture, randomUUID()), filename = join(directory, `owned-controls.${extension}`);
  await writeFile(filename, bytes); const source = {sha256: sha(bytes), sizeBytes: bytes.length}; report.source = source;
  report.inputRecipe = {recipe: core === "tic80" ? "tic-dummy-chunk-v1" : "pico-ignored-metadata-v1", sourceSha256: sha(fixture), sourceSizeBytes: fixture.length};
  let sourceSha256 = source.sha256;
  if (env.RETROM_CONTENT_IO_SOURCE_RECEIPTS) {
    const receipts = JSON.parse(await readFile(env.RETROM_CONTENT_IO_SOURCE_RECEIPTS, "utf8"));
    assert.equal(receipts.filter(row => row.role === "owned-game" && row.sha256 === sha(fixture) &&
      row.sizeBytes === fixture.length && row.files === 1).length, 1, "CONTENT_IO_PERFORMANCE_SOURCE_CHANGED");
    sourceSha256 = sourceReceiptDigest(receipts); report.sourceReceiptSha256 = sourceSha256;
  }
  const browserSha256 = sha(await readFile(env.RETROM_CHROME_EXECUTABLE));
  const networkSettingsSha256 = sha(JSON.stringify({chromeArgs: spec.chromeArgs, viewport: [1280, 900], network: "unthrottled-loopback-proxy"}));
  const games = {};
  for (const variant of ["baseline", "candidate"]) {
    const opened = await open(variant); games[variant] = (await publish(opened.client, filename)).gameId;
    const measured = await measureFantasyLaunch({browser, ...opened, base: spec[variant].baseURL,
      gameId: games[variant], core, variant, source, assets: assets[variant], directory});
    assert.equal(measured.runtime.bundleSha256, spec[variant].bundleSha256);
    assert.equal(measured.runtime.moduleSha256, spec[variant].moduleSha256);
    report.warmup.push({variant, reason: "Compile PFB development routes before timed repetitions", ...measured});
    await close();
  }
  for (let repetition = 0; repetition < 5; repetition++) for (const variant of ["baseline", "candidate"]) {
    const opened = await open(variant), contextId = randomUUID();
    for (const cacheState of ["cold", "warm"]) {
      const measured = await measureFantasyLaunch({browser, ...opened, base: spec[variant].baseURL,
        gameId: games[variant], core, variant, source, assets: assets[variant], directory});
      assert.equal(measured.runtime.bundleSha256, spec[variant].bundleSha256);
      assert.equal(measured.runtime.moduleSha256, spec[variant].moduleSha256);
      const runId = randomUUID(); await writeFile(join(directory, `${runId}.json`), JSON.stringify(measured, null, 2) + "\n");
      report.samples.push({caseId: report.caseId, runId, variant, cacheState, repetition, launchId: measured.launchId, contextId,
        sourceSha256, browserSha256, networkSettingsSha256, providerBundleSha256: measured.runtime.bundleSha256,
        observationId: fantasyObservation(core), metrics: measured.metrics});
      await writeFile(join(directory, "performance.json"), JSON.stringify(report, null, 2) + "\n");
      console.log(`${variant} ${cacheState} ${repetition}: frame=${measured.metrics.firstFrameMs.toFixed(1)} input=${measured.metrics.inputReadyMs.toFixed(1)}`);
    }
    await close();
  }
  report.comparison = compareContentIOPerformance(report.caseId, report.samples); report.status = report.comparison.status;
  assert.equal(report.status, "PASS", "CONTENT_IO_PERFORMANCE_REGRESSION"); await finish();
} catch (error) {await finish(error);}
