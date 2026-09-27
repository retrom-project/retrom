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
import {pauseComputer, closeComputer} from "./computer_product_browser.mjs";
import {evictDOSMemory} from "./dosbox_native_trace.mjs";
import {corruptDOSBlock, publishDOSBacking, inspectDOSQuarantine} from "./dosbox_cache_faults.mjs";
import {observeDOSFailure} from "./dosbox_failure_observation.mjs";
import {collectDOSFailure} from "./dosbox_failure_metrics.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL, directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR);
await mkdir(directory, {recursive: true});
const input = JSON.parse(await readFile(env.RETROM_CONTENT_IO_DOS_INPUT, "utf8"));
const provider = await readPFBProvider(process.cwd(), "emulatorjs", "dosbox-pure", {nativeBaseline: "candidate"});
const report = {schemaVersion: 1, caseId: "ACC-DOSBOX-001", runId: env.RETROM_CONTENT_IO_RUN_ID,
  scenario: "cache-corruption", status: "FAIL", launches: []};
let browser, proxy, active, worker, owner, failure;
async function start(context, client) {
  const launch = await launchCart(client, input.gameId, null, env.RETROM_DOS_ENTRY); launch.returnTo = `/games/${input.gameId}`;
  const opened = active = await openDOS(context, base, launch, async page => {
    owner = await observeDOSContentOwner(context, page, provider.files.candidate.get("client.mjs").toString());
    worker = await observeDOSWorker(context, page, provider.files.candidate.get("assets/content-io/worker.mjs").toString());
  });
  await owner.finish(); owner = null;
  assert.equal(opened.contentDigest, input.content.contentDigest);
  await bootDoom(opened); await moveAndFireDoom(opened); await pauseComputer(opened);
  await installDOSNativeReader(opened.frame); return opened;
}
try {
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(15000); await installVirtualStandardGamepad(context);
  const collector = await observeContentStoreEvents(context, {retain: true}), client = await fantasyClient(context, base);
  const original = await start(context, client);
  report.partial = await corruptDOSBlock(worker, "PARTIAL");
  await worker.finish(); worker = null; report.launches.push(await closeComputer(original, base, collector));
  const replay = await start(context, client);
  assert.notEqual(replay.launch.launchId, original.launch.launchId);
  report.replay = await worker.evaluate(`(() => {
    const service = globalThis.__dosObservedService, object = [...service.files.values()].find(file => file.object.source.purpose === "GAME").object;
    return {corruptBlocks: service.store.stats.corruptBlocks, networkBytes: service.store.stats.networkBytes, revoked: object.state.revoked};
  })()`);
  assert.ok(report.replay.corruptBlocks > 0 && report.replay.networkBytes > 0); assert.equal(report.replay.revoked, false);
  report.materialized = await publishDOSBacking(replay);
  report.published = await corruptDOSBlock(worker, "COMPLETE"); assert.ok(report.published.resultLeases > 0);
  report.eviction = await evictDOSMemory(replay, worker);
  failure = await observeDOSFailure(context, replay.page, provider.files.candidate.get("client.mjs").toString(),
    () => inspectDOSQuarantine(worker, report.published));
  report.native = await replay.frame.evaluate(async () => {
    const result = await globalThis.__dosNativeRead(0, 1); return {code: result.code, copied: result.copied};
  });
  assert.deepEqual(report.native, {code: 29, copied: 0});
  report.quarantine = await failure.finish(); failure = null;
  report.worker = await worker.finish(); worker = null;
  report.launches.push(await collectDOSFailure(replay, collector, report.worker)); report.status = "PASS";
} catch (error) {
  report.errorCode = error.message; report.stack = error.stack; process.exitCode = 1;
  report.worker = worker?.snapshot(); report.failure = failure?.snapshot();
  await active?.page.screenshot({path: join(directory, "failure.png")}).catch(() => {});
} finally {
  await failure?.finish().catch(() => {}); await owner?.finish().catch(() => {}); await worker?.finish().catch(() => {});
  await browser?.close(); await proxy?.close();
  await writeFile(join(directory, "cache-corruption-product.json"), JSON.stringify(report, null, 2) + "\n");
  console.log(JSON.stringify({caseId: report.caseId, status: report.status, errorCode: report.errorCode}));
}
