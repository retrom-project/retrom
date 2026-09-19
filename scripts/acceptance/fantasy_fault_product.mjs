import assert from "node:assert/strict";
import {mkdir, readFile, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {proofDigest} from "./content_io_case_proof.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {observeContentStoreEvents, selectedContentBackend} from "./content_store_events.mjs";
import {fantasyClient} from "./fantasy_product_client.mjs";
import {fantasyPerformanceAssets} from "./fantasy_performance_assets.mjs";
import {measureFantasyLaunch} from "./fantasy_performance_browser.mjs";
import {denyContentWorkerStorage} from "./content_io_storage_denial.mjs";
import {pauseContentCommit} from "./content_io_commit_pause.mjs";
import {validateContentResources} from "./content_io_performance.mjs";

const core = process.argv[2], scenario = process.argv[3];
assert.ok(["tic80", "fake08"].includes(core) && ["cache-denied", "eager-progress"].includes(scenario));
const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL, directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR);
const input = JSON.parse(await readFile(env.RETROM_CONTENT_IO_FANTASY_INPUT, "utf8")); assert.equal(input.core, core);
await mkdir(directory, {recursive: false});
const report = {schemaVersion: 1, caseId: core === "tic80" ? "ACC-TIC-001" : "ACC-PICO-001",
  runId: env.RETROM_CONTENT_IO_RUN_ID ?? null, core, scenario, status: "FAIL", launches: []};
let browser, proxy, pause;
async function commitObservation(page, values) {
  assert.equal(values.kind, "BYTES"); assert.equal(values.size, input.source.sizeBytes);
  const progress = page.getByRole("progressbar"); await progress.waitFor({state: "visible", timeout: 5000});
  const percentage = Number(await progress.getAttribute("aria-valuenow"));
  assert.ok(Number.isFinite(percentage) && percentage >= 0 && percentage < 100, "CONTENT_IO_PREMATURE_COMPLETE_PROGRESS");
  const loadingVisible = await page.locator(".player-loading").isVisible(); assert.equal(loadingVisible, true);
  let canvasCount = 0; for (const frame of page.frames()) canvasCount += await frame.locator("canvas").count();
  assert.equal(canvasCount, 0, "CONTENT_IO_CORE_STARTED_BEFORE_COMMIT");
  await page.screenshot({path: join(directory, "before-commit.png")}); return {percentage, loadingVisible, canvasCount};
}
try {
  const assets = await fantasyPerformanceAssets(input.provider, core);
  const total = input.source.sizeBytes + assets.reduce((sum, row) => sum + row.sizeBytes, 0);
  const workerSource = await readFile(input.workerPath, "utf8"); assert.equal(proofDigest(workerSource), input.workerSha256);
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context);
  const collector = await observeContentStoreEvents(context, {retain: true}), client = await fantasyClient(context, base);
  for (let iteration = 0; iteration < (scenario === "cache-denied" ? 2 : 1); iteration++) {
    let fault;
    const observed = await measureFantasyLaunch({browser, context, collector, client, base, core, gameId: input.gameId,
      variant: "candidate", source: input.source, assets, directory, preparePage: async page => {
        if (scenario === "cache-denied") fault = await denyContentWorkerStorage(context, page);
        else pause = await pauseContentCommit(context, page, workerSource, values => commitObservation(page, values));
      }});
    const injection = scenario === "cache-denied" ? await fault.finish() : await pause.finish();
    report.launches.push({iteration, injection, ...observed});
    assert.equal(observed.runtime.bundleSha256, input.provider.bundleSha256);
    assert.equal(observed.runtime.moduleSha256, input.provider.moduleSha256);
    validateContentResources(observed.metrics.publicPeak, false); validateContentResources(observed.metrics.closed, true);
    assert.equal(observed.metrics.materializedBytes, total);
    if (scenario === "cache-denied") {
      assert.equal(selectedContentBackend(observed.storeEvents), "MEMORY");
      assert.equal(observed.metrics.wholeRequests, 3); assert.equal(observed.metrics.networkBytes, total);
    }
  }
  if (scenario === "cache-denied") assert.notEqual(report.launches[0].launchId, report.launches[1].launchId);
  report.status = "PASS";
} catch (error) {report.errorCode = error.message; report.stack = error.stack; report.pauseState = pause?.snapshot(); process.exitCode = 1;}
finally {
  await browser?.close(); await proxy?.close();
  await writeFile(join(directory, `${scenario}-product.json`), JSON.stringify(report, null, 2) + "\n");
}
