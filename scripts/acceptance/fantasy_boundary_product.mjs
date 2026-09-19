import assert from "node:assert/strict";
import {createHash, randomUUID} from "node:crypto";
import {mkdir, readFile, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {fantasyClient, previewCart, approveCart, importCart} from "./fantasy_product_client.mjs";
import {fantasyRunCart, fantasyBoundaryCart} from "./fantasy_run_cart.mjs";
import {fantasyPerformanceAssets} from "./fantasy_performance_assets.mjs";
import {measureFantasyLaunch} from "./fantasy_performance_browser.mjs";
import {observeContentIO, contentSourceMatcher} from "./content_io_observation.mjs";
import {validateContentResources} from "./content_io_performance.mjs";

const core = process.argv[2]; assert.ok(["tic80", "fake08"].includes(core));
const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL, directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR);
const input = JSON.parse(await readFile(env.RETROM_CONTENT_IO_FANTASY_INPUT, "utf8")); assert.equal(input.core, core);
await mkdir(directory, {recursive: false});
const report = {schemaVersion: 1, caseId: core === "tic80" ? "ACC-TIC-001" : "ACC-PICO-001", runId: env.RETROM_CONTENT_IO_RUN_ID ?? null, scenario: "size-boundaries", status: "FAIL", inputs: []};
let browser, proxy;
async function rejectOversized(context, client, collector, review) {
  const preview = await previewCart(client, review.itemId), page = await context.newPage();
  const config = await client.json("GET", `/runtime/launches/${preview.previewId}/config`);
  const game = config.resources.find(row => row.role === "game"); assert.equal(game.sizeBytes, 4194305);
  const network = observeContentIO(context, contentSourceMatcher([game], base)), workers = [], closedWorkers = new Set();
  page.on("worker", worker => {workers.push(worker); worker.on("close", () => closedWorkers.add(worker));});
  try {
    await page.goto(base + preview.playUrl, {waitUntil: "domcontentloaded"});
    await page.getByText("FANTASY_RUNTIME_CONFIG_INVALID", {exact: true}).waitFor({timeout: 30000});
    await network.flush(); assert.deepEqual(network.requests, []);
    const deadline = performance.now() + 5000;
    while (page.workers().length && performance.now() < deadline) await page.waitForTimeout(10);
    assert.equal(page.workers().length, 0);
    assert.equal(workers.length, 1); assert.equal(closedWorkers.size, workers.length);
    const sessions = collector.snapshot(page);
    // Admission fails before opening any file. Provider failure force-terminates its
    // Worker; it cannot promise the normal CLOSE_SESSION diagnostic handshake.
    const cleanup = {kind: "FORCED_WORKER_TERMINATION", workersClosed: closedWorkers.size,
      closeMetrics: null, closeMetricsUnavailableReason: "ADMISSION_FAILURE_FORCED_SESSION"};
    for (const frame of page.frames()) assert.equal(await frame.locator("canvas").count(), 0);
    await page.screenshot({path: join(directory, "oversized-rejected.png")});
    const finished = await client.raw("POST", `/runtime/launches/${preview.previewId}/finish`, {
      headers: {Origin: base}, data: {clientSequence: 0, clientObservedAtMs: Date.now(), previousInterval: null}});
    assert.equal(finished.status(), 200);
    return {previewId: preview.previewId, errorCode: "FANTASY_RUNTIME_CONFIG_INVALID", gameBodyRequests: 0,
      workersCreated: workers.length, workersRemaining: page.workers().length, sessions, cleanup, canvasCount: 0, finished: true, runtime: config.runtime};
  } catch (error) {
    await page.screenshot({path: join(directory, "oversized-failed.png")}).catch(() => {});
    await writeFile(join(directory, "oversized-failed.json"), JSON.stringify({
      alerts: await page.getByRole("alert").allTextContents().catch(() => []), requests: network.requests,
      workersCreated: workers.length, sessions: collector.snapshot(page)}, null, 2)); throw error;
  } finally {network.close(); await page.close();}
}
try {
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context); const collector = await observeContentStoreEvents(context, {retain: true});
  const client = await fantasyClient(context, base);
  const assets = await fantasyPerformanceAssets(input.provider, core);
  const extension = core === "tic80" ? "tic" : "p8";
  const original = await readFile(new URL(`../../testdata/public-roms/fantasy-controls/controls.${extension}`, import.meta.url));
  assert.equal(createHash("sha256").update(original).digest("hex"), core === "tic80" ? "c637c1f3e6f24af56850448fcd6fd6e6c06fa4e40fd735c02582b2cff20fb029" : "9965557a27b806c95174d5aabedb97d166836c26d4ac3372ae2b6bd44c6558f3");
  for (const sizeBytes of [4194304, 4194305]) {
    const bytes = fantasyBoundaryCart(core, fantasyRunCart(core, original, randomUUID()), sizeBytes), filename = join(directory, `owned-${sizeBytes}.${extension}`);
    await writeFile(filename, bytes); const source = {sha256: createHash("sha256").update(bytes).digest("hex"), sizeBytes};
    const review = await importCart(client, core, filename); report.inputs.push({...source, itemId: review.itemId});
    if (sizeBytes === 4194305) {report.rejected = await rejectOversized(context, client, collector, review); continue;}
    const {gameId} = await approveCart(client, review.itemId);
    report.maximum = await measureFantasyLaunch({browser, context, collector, client, base, core, gameId, variant: "candidate", source, assets, directory});
    const total = sizeBytes + assets.reduce((sum, row) => sum + row.sizeBytes, 0);
    assert.equal(report.maximum.metrics.materializedBytes, total); assert.equal(report.maximum.metrics.networkBytes, total);
    assert.equal(report.maximum.metrics.wholeRequests, 3);
    validateContentResources(report.maximum.metrics.publicPeak, false); validateContentResources(report.maximum.metrics.closed, true);
  }
  assert.equal(report.maximum.runtime.bundleSha256, input.provider.bundleSha256);
  assert.equal(report.maximum.runtime.moduleSha256, input.provider.moduleSha256);
  assert.equal(report.maximum.runtime.bundleSha256, report.rejected.runtime.bundleSha256);
  assert.equal(report.maximum.runtime.moduleSha256, report.rejected.runtime.moduleSha256); report.status = "PASS";
} catch (error) {report.errorCode = error.message; report.stack = error.stack; process.exitCode = 1;}
finally {await browser?.close(); await proxy?.close(); await writeFile(join(directory, "boundary-product.json"), JSON.stringify(report, null, 2) + "\n");}
