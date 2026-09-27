import assert from "node:assert/strict";
import {readFile, writeFile, mkdir} from "node:fs/promises";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {fantasyClient, launchCart} from "./fantasy_product_client.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {readPFBProvider} from "./content_io_pfb_provider.mjs";
import {openDOS} from "./dosbox_product_browser.mjs";
import {observeDOSContentOwner, installDOSNativeReader} from "./dosbox_native_observation.mjs";
import {observeDOSWorker} from "./dosbox_worker_observation.mjs";
import {bootDoom, moveAndFireDoom} from "./dosbox_doom_actions.mjs";
import {pauseComputer, closeComputer, collectComputerExit} from "./computer_product_browser.mjs";
import {dosOracle, traceDOS} from "./dosbox_native_trace.mjs";
import {concurrentDOS, faultDOS} from "./dosbox_range_faults.mjs";
import {measureDOS} from "./dosbox_content_measurement.mjs";
import {observeDOSFailure, openDOSAncillary, inspectDOSRevocation} from "./dosbox_failure_observation.mjs";
import {denyContentWorkerStorage} from "./content_io_storage_denial.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL, scenario = process.argv[2];
assert.ok(["trace", "cache-denied", "fault-range-200", "fault-identity-412", "fault-short-body", "read-exit", "worker-termination"].includes(scenario));
const input = JSON.parse(await readFile(env.RETROM_CONTENT_IO_DOS_INPUT, "utf8")), directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR);
await mkdir(directory, {recursive: true});
const provider = await readPFBProvider(process.cwd(), "emulatorjs", "dosbox-pure");
const report = {schemaVersion: 1, caseId: "ACC-DOSBOX-001", runId: env.RETROM_CONTENT_IO_RUN_ID, status: "FAIL", scenario, launches: []};
let browser, proxy, active, worker, owner, failure;
try {
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(15000); await installVirtualStandardGamepad(context);
  const collector = await observeContentStoreEvents(context, {retain: true}), client = await fantasyClient(context, base);
  if (scenario === "cache-denied") {
    for (let iteration = 0; iteration < 2; iteration++) {
      let denial;
      const result = await measureDOS({browser, context, collector, client, base, ...input, directory,
        preparePage: async page => {denial = await denyContentWorkerStorage(context, page);}});
      result.injection = await denial.finish(); assert.ok(result.metrics.networkBytes > 0);
      assert.equal(Object.values(result.sessions)[0].latest.cacheBytesL2, 0);
      report.launches.push(result);
    }
  } else {
    const launch = await launchCart(client, input.gameId, null, env.RETROM_DOS_ENTRY); launch.returnTo = `/games/${input.gameId}`;
    active = await openDOS(context, base, launch, async page => {
      owner = await observeDOSContentOwner(context, page, provider.files.candidate.get("client.mjs").toString());
      worker = await observeDOSWorker(context, page, provider.files.candidate.get("assets/content-io/worker.mjs").toString(), true);
    });
    report.owner = await owner.finish(); owner = null;
    assert.equal(active.contentDigest, input.content.contentDigest); assert.equal(active.source.sizeBytes, input.content.sizeBytes);
    report.boot = await bootDoom(active); report.input = await moveAndFireDoom(active);
    await pauseComputer(active); report.mount = await installDOSNativeReader(active.frame);
    if (scenario === "trace") {
      const oracle = await dosOracle(active); report.oracle = oracle.receipt;
      report.concurrent = await concurrentDOS(active, worker, oracle.bytes);
      Object.assign(report, await traceDOS(active, oracle.bytes));
      await active.flush(); report.ranges = active.rangeSummary();
    } else {
      if (scenario === "fault-identity-412") {
        report.ancillary = await openDOSAncillary(active);
        failure = await observeDOSFailure(context, active.page, provider.files.candidate.get("client.mjs").toString(),
          () => inspectDOSRevocation(worker, report.ancillary));
      }
      report.fault = await faultDOS(active, worker, scenario, base);
      if (failure) {report.revocation = await failure.finish(); failure = null;}
    }
    report.worker = await worker.finish(); worker = null;
    report.launches.push(await (scenario === "read-exit" ? collectComputerExit(active, collector) : closeComputer(active, base, collector)));
  }
  assert.equal((await readPFBProvider(process.cwd(), "emulatorjs", "dosbox-pure")).developmentSha256, provider.developmentSha256);
  report.status = "PASS";
} catch (error) {
  report.errorCode = error.message; report.stack = error.stack; process.exitCode = 1;
  report.revocation = failure?.snapshot(); report.worker = worker?.snapshot(); report.owner = owner?.snapshot();
  if (active) {
    await active.page.screenshot({path: join(directory, "failure.png")}).catch(() => {});
    report.text = await active.page.locator("body").innerText().catch(() => "");
  }
} finally {
  await failure?.finish().catch(() => {}); await owner?.finish().catch(() => {}); await worker?.finish().catch(() => {});
  await browser?.close(); await proxy?.close();
  await writeFile(join(directory, `${scenario}-product.json`), JSON.stringify(report, null, 2) + "\n");
  console.log(JSON.stringify({caseId: report.caseId, scenario, status: report.status, errorCode: report.errorCode}));
}
