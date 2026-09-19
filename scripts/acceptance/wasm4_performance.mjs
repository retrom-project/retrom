import assert from "node:assert/strict";
import {sourceReceiptDigest} from "./content_io_case_proof.mjs";
import {createHash, randomUUID} from "node:crypto";
import {mkdir, readFile, writeFile} from "node:fs/promises";
import {resolve, join} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {fantasyClient, approveCart} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {runCart} from "./wasm4_run_cart.mjs";
import {compareContentIOPerformance} from "./content_io_performance.mjs";
import {measureWasm4Launch, observeWasm4Delivery, wasm4Observation} from "./wasm4_performance_browser.mjs";

const sha = bytes => createHash("sha256").update(bytes).digest("hex");
const env = process.env, directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR);
const spec = JSON.parse(await readFile(env.RETROM_CONTENT_IO_PERFORMANCE_INPUT, "utf8"));
await mkdir(directory, {recursive: false});
const report = {schemaVersion: 1, caseId: "ACC-WASM4-001", runId: env.RETROM_CONTENT_IO_RUN_ID ?? null, status: "FAIL", observationId: wasm4Observation, samples: [], warmup: []};
let browser, proxy;
const watchdog = setTimeout(() => {void finish(new Error("CONTENT_IO_PERFORMANCE_TIMEOUT")).finally(() => process.exit(1));}, 300000);
async function finish(error) {
  clearTimeout(watchdog);
  if (error) {report.errorCode = error.message; report.stack = error.stack; process.exitCode = 1;}
  await browser?.close(); await proxy?.close();
  await writeFile(join(directory, "performance.json"), JSON.stringify(report, null, 2) + "\n");
}
async function open(variant, source) {
  proxy = await localRpgAcceptanceProxy(spec[variant].baseURL);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true, args: spec.chromeArgs});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context); await observeWasm4Delivery(context, source);
  const collector = await observeContentStoreEvents(context, {retain: true});
  const client = await fantasyClient(context, spec[variant].baseURL);
  return {context, collector, client};
}
async function close() {await browser.close(); browser = null; await proxy.close(); proxy = null;}
async function publish(client, filename) {
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {headers: client.writeHeaders(), data: {}});
  const rows = await client.json("GET", "/api/v1/admin/platform-instances?platformId=wasm4&limit=100");
  const instance = rows.items.find(row => row.enabled && row.defaultCoreId === "wasm4"); assert.ok(instance);
  const uploadId = await client.upload(singleFile(filename), "FILES", "GENERAL");
  const imported = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []}});
  return approveCart(client, (await reviewForImport(client, imported.importJobId)).itemId);
}
try {
  assert.ok(env.RETROM_CHROME_EXECUTABLE && env.RETROM_ACCEPTANCE_USERNAME && env.RETROM_ACCEPTANCE_PASSWORD);
  assert.notEqual(spec.baseline.baseURL, spec.candidate.baseURL);
  assert.notEqual(spec.baseline.bundleSha256, spec.candidate.bundleSha256);
  assert.deepEqual(spec.chromeArgs, ["--use-angle=swiftshader", "--enable-unsafe-swiftshader"]);
  const fixture = await readFile(new URL("../../testdata/public-roms/wasm4-controls/controls.wasm", import.meta.url));
  assert.equal(sha(fixture), "c19447a62cb51bbe9b91e3ef3002c598972bd20bfb85f96668e5c05e85e256cd");
  const bytes = runCart(fixture, randomUUID()), filename = join(directory, "owned-controls.wasm");
  await writeFile(filename, bytes); const source = {sha256: sha(bytes), sizeBytes: bytes.length}; report.source = source;
  report.inputRecipe = {recipe: "wasm-custom-section-v1", sourceSha256: sha(fixture), sourceSizeBytes: fixture.length};
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
    const opened = await open(variant, source); games[variant] = (await publish(opened.client, filename)).gameId;
    const measured = await measureWasm4Launch({browser, ...opened, base: spec[variant].baseURL,
      gameId: games[variant], variant, source, directory});
    assert.equal(measured.runtime.bundleSha256, spec[variant].bundleSha256);
    assert.equal(measured.runtime.moduleSha256, spec[variant].moduleSha256);
    report.warmup.push({variant, reason: "Compile PFB development routes before timed repetitions", ...measured});
    await close();
  }
  for (let repetition = 0; repetition < 5; repetition++) for (const variant of ["baseline", "candidate"]) {
    const opened = await open(variant, source), contextId = randomUUID();
    for (const cacheState of ["cold", "warm"]) {
      const measured = await measureWasm4Launch({browser, ...opened, base: spec[variant].baseURL,
        gameId: games[variant], variant, source, directory});
      assert.equal(measured.runtime.bundleSha256, spec[variant].bundleSha256);
      assert.equal(measured.runtime.moduleSha256, spec[variant].moduleSha256);
      const runId = randomUUID(); await writeFile(join(directory, `${runId}.json`), JSON.stringify(measured, null, 2) + "\n");
      report.samples.push({caseId: report.caseId, runId, variant, cacheState, repetition, launchId: measured.launchId, contextId,
        sourceSha256, browserSha256, networkSettingsSha256, providerBundleSha256: measured.runtime.bundleSha256,
        observationId: wasm4Observation, metrics: measured.metrics});
      await writeFile(join(directory, "performance.json"), JSON.stringify(report, null, 2) + "\n");
      console.log(`${variant} ${cacheState} ${repetition}: frame=${measured.metrics.firstFrameMs.toFixed(1)} input=${measured.metrics.inputReadyMs.toFixed(1)}`);
    }
    await close();
  }
  report.comparison = compareContentIOPerformance(report.caseId, report.samples); report.status = report.comparison.status;
  assert.equal(report.status, "PASS", "CONTENT_IO_PERFORMANCE_REGRESSION"); await finish();
} catch (error) {await finish(error);}
