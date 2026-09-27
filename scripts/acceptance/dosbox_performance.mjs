import assert from "node:assert/strict";
import {readFile, writeFile, mkdir} from "node:fs/promises";
import {join, resolve} from "node:path";
import {randomUUID} from "node:crypto";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {fantasyClient} from "./fantasy_product_client.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {readPFBProvider, selectFrozenProvider} from "./content_io_pfb_provider.mjs";
import {proofDigest, sourceReceiptDigest} from "./content_io_case_proof.mjs";
import {compareContentIOPerformance} from "./content_io_performance.mjs";
import {measureDOS, dosObservation} from "./dosbox_content_measurement.mjs";
import {selectDOSPerformanceBaseline} from "./dosbox_performance_baseline.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL, directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR);
await mkdir(directory, {recursive: false});
const input = JSON.parse(await readFile(env.RETROM_CONTENT_IO_DOS_INPUT, "utf8"));
const provider = await readPFBProvider(process.cwd(), "emulatorjs", "dosbox-pure", {nativeBaseline: "candidate"});
const baseline = await selectDOSPerformanceBaseline(provider, env.RETROM_DOS_PERFORMANCE_BASELINE);
const report = {schemaVersion: 1, caseId: "ACC-DOSBOX-001", runId: env.RETROM_CONTENT_IO_RUN_ID, status: "FAIL",
  samples: [], warmup: [], observationId: dosObservation, baseline,
  provider: {identities: provider.identities, nativeAssets: provider.nativeAssets, nativeBaseline: provider.nativeBaseline, installedNativeAssets: provider.installedNativeAssets, developmentSha256: provider.developmentSha256}};
let browser, proxy;
const chromeArgs = ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"];
async function profile(variant) {
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(15000); await installVirtualStandardGamepad(context);
  const collector = await observeContentStoreEvents(context, {retain: true}), client = await fantasyClient(context, base);
  const delivered = await selectFrozenProvider(context, base, provider, variant);
  return {context, collector, client, delivered};
}
async function measure(opened, variant) {
  const result = await measureDOS({browser, ...opened, base, gameId: input.gameId, content: input.content, directory});
  assert.equal(result.runtime.moduleSha256, provider.identities[variant].moduleSha256);
  assert.equal(result.runtime.bundleSha256, provider.identities[variant].bundleSha256);
  assert.ok(result.assets.some(row => row.path.endsWith("/assets/content-io/worker.mjs") && row.sha256 === provider.identities[variant].workerSha256));
  return result;
}
try {
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true, args: chromeArgs});
  const browserSha256 = proofDigest(await readFile(env.RETROM_CHROME_EXECUTABLE));
  const receipts = JSON.parse(await readFile(input.receiptsPath, "utf8")); report.sourceReceiptSha256 = sourceReceiptDigest(receipts);
  const networkSettingsSha256 = proofDigest(JSON.stringify({chromeArgs, viewport: [1280, 900], network: "unthrottled-loopback-proxy", provider: "verified-frozen-files"}));
  for (let repetition = 0; repetition < 5; repetition++) for (const variant of ["baseline", "candidate"]) {
    const opened = await profile(variant), contextId = randomUUID();
    for (const cacheState of ["cold", "warm"]) {
      const result = await measure(opened, variant), runId = randomUUID();
      if (cacheState === "warm") assert.equal(result.requests.length, 0, "DOS_PERFORMANCE_WARM_BODY");
      await writeFile(join(directory, `${runId}.json`), JSON.stringify({...result, delivered: opened.delivered}, null, 2));
      report.samples.push({caseId: report.caseId, runId, variant, cacheState, repetition, launchId: result.launchId, contextId,
        sourceSha256: report.sourceReceiptSha256, browserSha256, networkSettingsSha256,
        providerBundleSha256: result.runtime.bundleSha256, providerModuleSha256: result.runtime.moduleSha256,
        observationId: dosObservation, metrics: result.metrics});
      console.log(`dosbox-pure ${variant} ${cacheState} ${repetition}: ${result.metrics.inputReadyMs.toFixed(0)} ms`);
      await writeFile(join(directory, "performance-product.json"), JSON.stringify(report, null, 2));
    }
    await opened.context.close();
  }
  report.comparison = compareContentIOPerformance(report.caseId, report.samples);
  assert.equal(report.comparison.status, "PASS", "DOS_PERFORMANCE_REGRESSION");
  assert.equal((await readPFBProvider(process.cwd(), "emulatorjs", "dosbox-pure", {nativeBaseline: "candidate"})).developmentSha256, provider.developmentSha256);
  report.status = "PASS";
} catch (error) {report.errorCode = error.message; report.stack = error.stack; process.exitCode = 1;}
finally {
  await browser?.close(); await proxy?.close();
  await writeFile(join(directory, "performance-product.json"), JSON.stringify(report, null, 2) + "\n");
  console.log(JSON.stringify({caseId: report.caseId, status: report.status, errorCode: report.errorCode}));
}
