import assert from "node:assert/strict";
import {mkdir, readFile, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {observeContentStoreEvents, selectedContentBackend} from "./content_store_events.mjs";
import {fantasyClient} from "./fantasy_product_client.mjs";
import {measureWasm4Launch} from "./wasm4_performance_browser.mjs";
import {validateContentResources} from "./content_io_performance.mjs";
import {denyContentWorkerStorage} from "./content_io_storage_denial.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR);
const input = JSON.parse(await readFile(env.RETROM_CONTENT_IO_STORAGE_INPUT, "utf8"));
await mkdir(directory, {recursive: false});
const report = {schemaVersion: 1, caseId: "ACC-WASM4-001", runId: env.RETROM_CONTENT_IO_RUN_ID ?? null, scenario: "cache-denied", status: "FAIL", launches: []};
let browser, proxy;
try {
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context);
  const collector = await observeContentStoreEvents(context, {retain: true}), client = await fantasyClient(context, base);
  for (const state of ["cold", "new-launch"]) {
    let fault;
    const observed = await measureWasm4Launch({browser, context, collector, client, base, gameId: input.gameId,
      variant: "candidate", source: input.source, directory,
      preparePage: async page => {fault = await denyContentWorkerStorage(context, page);}});
    const denial = await fault.finish();
    report.launches.push({state, denial, ...observed});
    assert.equal(observed.runtime.bundleSha256, input.bundleSha256); assert.equal(observed.runtime.moduleSha256, input.moduleSha256);
    assert.equal(observed.metrics.wholeRequests, 1); assert.equal(observed.metrics.networkBytes, input.source.sizeBytes);
    assert.equal(selectedContentBackend(observed.storeEvents), "MEMORY", "CONTENT_IO_MEMORY_FALLBACK_MISSING");
    validateContentResources(observed.metrics.publicPeak, false); validateContentResources(observed.metrics.closed, true);
  }
  assert.notEqual(report.launches[0].launchId, report.launches[1].launchId); report.status = "PASS";
} catch (error) {report.errorCode = error.message; report.stack = error.stack; process.exitCode = 1;}
finally {
  await browser?.close(); await proxy?.close();
  await writeFile(join(directory, "storage-product.json"), JSON.stringify(report, null, 2) + "\n");
}
