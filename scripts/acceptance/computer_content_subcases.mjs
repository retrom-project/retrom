import assert from "node:assert/strict";
import {readFile, writeFile, mkdir} from "node:fs/promises";
import {join, resolve} from "node:path";
import {randomUUID} from "node:crypto";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {fantasyClient} from "./fantasy_product_client.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {observeBBC} from "./bbc_product_browser.mjs";
import {readPFBProvider, selectFrozenProvider} from "./content_io_pfb_provider.mjs";
import {computerObservation, measureComputer} from "./computer_content_measurement.mjs";
import {denyContentWorkerStorage} from "./content_io_storage_denial.mjs";
import {pauseContentCommit} from "./content_io_commit_pause.mjs";
import {proofDigest, sourceReceiptDigest} from "./content_io_case_proof.mjs";
import {compareContentIOPerformance} from "./content_io_performance.mjs";

const env = process.env, [target, scenario] = process.argv.slice(2), base = env.RETROM_ACCEPTANCE_BASE_URL;
assert.ok(["bbc-jsbeeb", "samcoupe"].includes(target) && ["cache-denied", "eager-progress", "performance"].includes(scenario));
const input = JSON.parse(await readFile(env.RETROM_CONTENT_IO_COMPUTER_INPUT, "utf8"));
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR); await mkdir(directory, {recursive: true});
const caseId = target === "bbc-jsbeeb" ? "ACC-BBC-001" : "ACC-SAMCOUPE-001";
const report = {schemaVersion: 1, caseId, runId: env.RETROM_CONTENT_IO_RUN_ID, status: "FAIL", scenario, launches: [], samples: [], warmup: []};
const provider = await readPFBProvider(process.cwd(), "retrom-runtime", target);
report.provider = {identities: provider.identities, developmentSha256: provider.developmentSha256, nativeAssets: provider.nativeAssets};
let browser, proxy, fault;
const chromeArgs = ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"];
async function profile(variant) {
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(15000); await installVirtualStandardGamepad(context);
  if (target === "bbc-jsbeeb") await observeBBC(context);
  const collector = await observeContentStoreEvents(context, {retain: true}), client = await fantasyClient(context, base);
  const delivered = scenario === "performance" ? await selectFrozenProvider(context, base, provider, variant) : [];
  return {context, collector, client, delivered};
}
async function commitObservation(page, values) {
  assert.equal(values.kind, "BYTES"); assert.equal(values.size, input.sources[0].sizeBytes);
  const progress = page.getByRole("progressbar"); await progress.waitFor({state: "visible", timeout: 5000});
  const percentage = Number(await progress.getAttribute("aria-valuenow"));
  const loadingVisible = await page.locator(".player-loading").isVisible();
  assert.ok(percentage >= 0 && percentage < 100 && loadingVisible, "COMPUTER_EARLY_COMPLETE_PROGRESS");
  let canvasCount = 0; for (const frame of page.frames()) canvasCount += await frame.locator("canvas").count();
  assert.equal(canvasCount, 0, "COMPUTER_CORE_STARTED_BEFORE_COMMIT");
  await page.screenshot({path: join(directory, "before-commit.png")}); return {percentage, loadingVisible, canvasCount};
}
async function measure(opened, variant) {
  const result = await measureComputer({browser, ...opened, base, target, gameId: input.gameId, sources: input.sources, directory,
    preparePage: async page => {
      if (scenario === "cache-denied") fault = await denyContentWorkerStorage(opened.context, page);
      if (scenario === "eager-progress") fault = await pauseContentCommit(opened.context, page,
        provider.files.candidate.get("assets/content-io/worker.mjs").toString(), values => commitObservation(page, values));
    }});
  assert.equal(result.runtime.bundleSha256, provider.identities[variant].bundleSha256);
  assert.equal(result.runtime.moduleSha256, provider.identities[variant].moduleSha256);
  if (scenario !== "performance") result.injection = await fault.finish();
  return result;
}
try {
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true, args: chromeArgs});
  const browserSha256 = proofDigest(await readFile(env.RETROM_CHROME_EXECUTABLE));
  const receipts = JSON.parse(await readFile(input.receiptsPath, "utf8"));
  report.sourceReceiptSha256 = sourceReceiptDigest(receipts); report.observationId = computerObservation(target);
  const networkSettingsSha256 = proofDigest(JSON.stringify({chromeArgs, viewport: [1280, 900], network: "unthrottled-loopback-proxy", provider: "verified-frozen-files"}));
  if (scenario === "performance") {
    // Each isolated pair already contains its cold observation and warm replay.
    // Run exactly the twenty required samples; extra unmeasured full game boots
    // consume the bounded native-computer case budget without adding coverage.
    for (let repetition = 0; repetition < 5; repetition++) for (const variant of ["baseline", "candidate"]) {
      const opened = await profile(variant), contextId = randomUUID();
      for (const cacheState of ["cold", "warm"]) {
        const result = await measure(opened, variant), runId = randomUUID();
        if (cacheState === "warm") assert.equal(result.requests.length, 0, "COMPUTER_PERFORMANCE_WARM_BODY");
        await writeFile(join(directory, `${runId}.json`), JSON.stringify({...result, delivered: opened.delivered}, null, 2));
        report.samples.push({caseId, runId, variant, cacheState, repetition, launchId: result.launchId, contextId,
          sourceSha256: report.sourceReceiptSha256, browserSha256, networkSettingsSha256,
          providerBundleSha256: result.runtime.bundleSha256, providerModuleSha256: result.runtime.moduleSha256,
          observationId: report.observationId, metrics: result.metrics});
        console.log(`${target} ${variant} ${cacheState} ${repetition}: ${result.metrics.inputReadyMs.toFixed(0)} ms`);
        await writeFile(join(directory, `${scenario}-product.json`), JSON.stringify(report, null, 2));
      }
      await opened.context.close();
    }
    report.comparison = compareContentIOPerformance(caseId, report.samples);
    assert.equal(report.comparison.status, "PASS", "COMPUTER_PERFORMANCE_REGRESSION");
  } else {
    const opened = await profile("candidate");
    for (let iteration = 0; iteration < (scenario === "cache-denied" ? 2 : 1); iteration++) {
      const result = await measure(opened, "candidate"); report.launches.push(result);
      if (scenario === "cache-denied") {
        assert.equal(Object.values(result.sessions)[0].latest.cacheBytesL2, 0);
        assert.equal(result.metrics.networkBytes, input.sources.reduce((sum, row) => sum + row.sizeBytes, 0));
        assert.equal(result.metrics.wholeRequests, input.sources.length);
      }
    }
    await opened.context.close();
  }
  const after = await readPFBProvider(process.cwd(), "retrom-runtime", target);
  assert.equal(after.developmentSha256, provider.developmentSha256, "COMPUTER_PROVIDER_CHANGED"); report.status = "PASS";
} catch (error) {
  report.errorCode = error.message; report.stack = error.stack; report.injection = fault?.snapshot?.(); process.exitCode = 1;
  report.failures = [];
  for (const context of browser?.contexts() ?? []) for (const page of context.pages()) {
    const filename = `failure-${report.failures.length}.png`;
    await page.screenshot({path: join(directory, filename)}).catch(() => {});
    report.failures.push({screenshot: filename, text: await page.locator("body").innerText().catch(() => "")});
  }
}
finally {
  await browser?.close(); await proxy?.close();
  await writeFile(join(directory, `${scenario}-product.json`), JSON.stringify(report, null, 2) + "\n");
  console.log(JSON.stringify({caseId, scenario, status: report.status, errorCode: report.errorCode}));
}
