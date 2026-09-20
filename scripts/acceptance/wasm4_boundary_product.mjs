import assert from "node:assert/strict";
import {createHash, randomUUID} from "node:crypto";
import {mkdir, readFile, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {fantasyClient, previewCart, approveCart} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {runCart} from "./wasm4_run_cart.mjs";
import {wasm4BoundaryCart} from "./wasm4_boundary_cart.mjs";
import {measureWasm4Launch} from "./wasm4_performance_browser.mjs";
import {observeContentIO, contentSourceMatcher} from "./content_io_observation.mjs";
import {validateContentResources} from "./content_io_performance.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL, directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR);
await mkdir(directory, {recursive: false});
const report = {schemaVersion: 1, caseId: "ACC-WASM4-001", runId: env.RETROM_CONTENT_IO_RUN_ID ?? null, scenario: "size-boundaries", status: "FAIL", inputs: []};
let browser, proxy;
async function importCart(client, filename, instance) {
  const uploadId = await client.upload(singleFile(filename), "FILES", "GENERAL");
  const imported = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []}});
  return reviewForImport(client, imported.importJobId);
}
async function rejectOversized(context, client, review) {
  const preview = await previewCart(client, review.itemId), page = await context.newPage();
  const config = await client.json("GET", `/runtime/launches/${preview.previewId}/config`);
  const game = config.resources.find(row => row.role === "game"); assert.equal(game.sizeBytes, 65537);
  const network = observeContentIO(context, contentSourceMatcher([game], base)), workers = [];
  page.on("worker", worker => workers.push(worker));
  try {
    await page.goto(base + preview.playUrl, {waitUntil: "domcontentloaded"});
    await page.getByText("PROVIDER_LAUNCH_REQUEST_INVALID", {exact: true}).waitFor({timeout: 30000});
    await network.flush(); assert.deepEqual(network.requests, []); assert.equal(workers.length, 0);
    for (const frame of page.frames()) assert.equal(await frame.locator("canvas").count(), 0);
    await page.screenshot({path: join(directory, "oversized-rejected.png")});
    const finished = await client.raw("POST", `/runtime/launches/${preview.previewId}/finish`, {
      headers: {Origin: base}, data: {clientSequence: 0, clientObservedAtMs: Date.now(), previousInterval: null}});
    assert.equal(finished.status(), 200);
    return {previewId: preview.previewId, errorCode: "PROVIDER_LAUNCH_REQUEST_INVALID", gameBodyRequests: 0,
      workersCreated: workers.length, canvasCount: 0, finished: true, runtime: config.runtime};
  } finally {network.close(); await page.close();}
}
try {
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context); const collector = await observeContentStoreEvents(context, {retain: true});
  const client = await fantasyClient(context, base);
  const rows = await client.json("GET", "/api/v1/admin/platform-instances?platformId=wasm4&limit=100");
  const instance = rows.items.find(row => row.enabled && row.defaultCoreId === "wasm4"); assert.ok(instance);
  const original = await readFile(new URL("../../testdata/public-roms/wasm4-controls/controls.wasm", import.meta.url));
  assert.equal(createHash("sha256").update(original).digest("hex"), "c19447a62cb51bbe9b91e3ef3002c598972bd20bfb85f96668e5c05e85e256cd");
  for (const sizeBytes of [65536, 65537]) {
    const bytes = wasm4BoundaryCart(runCart(original, randomUUID()), sizeBytes), filename = join(directory, `owned-${sizeBytes}.wasm`);
    await writeFile(filename, bytes); const source = {sha256: createHash("sha256").update(bytes).digest("hex"), sizeBytes};
    const review = await importCart(client, filename, instance); report.inputs.push({...source, itemId: review.itemId});
    if (sizeBytes === 65537) {report.rejected = await rejectOversized(context, client, review); continue;}
    const {gameId} = await approveCart(client, review.itemId);
    report.maximum = await measureWasm4Launch({browser, context, collector, client, base, gameId, variant: "candidate", source, directory});
    assert.equal(report.maximum.metrics.materializedBytes, sizeBytes); assert.equal(report.maximum.metrics.networkBytes, sizeBytes);
    assert.equal(report.maximum.metrics.wholeRequests, 1);
    validateContentResources(report.maximum.metrics.publicPeak, false); validateContentResources(report.maximum.metrics.closed, true);
  }
  assert.equal(report.maximum.runtime.bundleSha256, report.rejected.runtime.bundleSha256);
  assert.equal(report.maximum.runtime.moduleSha256, report.rejected.runtime.moduleSha256); report.status = "PASS";
} catch (error) {report.errorCode = error.message; report.stack = error.stack; process.exitCode = 1;}
finally {await browser?.close(); await proxy?.close(); await writeFile(join(directory, "boundary-product.json"), JSON.stringify(report, null, 2) + "\n");}
