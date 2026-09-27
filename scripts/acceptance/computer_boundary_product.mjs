import assert from "node:assert/strict";
import {readFile, writeFile, mkdir} from "node:fs/promises";
import {resolve, join} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {fantasyClient, launchCart} from "./fantasy_product_client.mjs";
import {computerResources} from "./computer_product_browser.mjs";
import {observeContentIO, contentSourceMatcher} from "./content_io_observation.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {readPFBProvider} from "./content_io_pfb_provider.mjs";
import {pauseContentCommit} from "./content_io_commit_pause.mjs";
import {proofDigest} from "./content_io_case_proof.mjs";

// This is an admission/materialization boundary test. Padding is an explicit test
// transformation; gameplay and native restore are verified on the original disks.
const env = process.env, target = process.argv[2], base = env.RETROM_ACCEPTANCE_BASE_URL;
assert.ok(["bbc-jsbeeb", "samcoupe"].includes(target));
const input = JSON.parse(await readFile(env.RETROM_CONTENT_IO_COMPUTER_INPUT, "utf8"));
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR); await mkdir(directory, {recursive: false});
const provider = await readPFBProvider(process.cwd(), "retrom-runtime", target);
const maximum = target === "bbc-jsbeeb" ? 33554432 : 16777216;
const report = {schemaVersion: 1, caseId: target === "bbc-jsbeeb" ? "ACC-BBC-001" : "ACC-SAMCOUPE-001",
  runId: env.RETROM_CONTENT_IO_RUN_ID, status: "FAIL", scope: "PRODUCT_ADMISSION_AND_MATERIALIZATION", inputs: []};
let browser, proxy;
try {
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  for (const sizeBytes of [maximum, maximum + 1]) {
    const context = await browser.newContext({...proxy.contextOptions, viewport: {width: 1280, height: 900}});
    const client = await fantasyClient(context, base), collector = await observeContentStoreEvents(context, {retain: true});
    const launch = await launchCart(client, input.gameId);
    const config = await client.json("GET", `/runtime/launches/${launch.launchId}/config`);
    const game = config.resources.find(row => row.kind === "ROM_BLOB"); assert.ok(game);
    const response = await context.request.get(new URL(game.url, base).href); assert.equal(response.status(), 200);
    const original = await response.body(); assert.equal(proofDigest(original), input.sources[0].sha256);
    const bytes = Buffer.alloc(sizeBytes); original.copy(bytes);
    game.sizeBytes = sizeBytes; game.sha256 = proofDigest(bytes);
    await context.route(`**/runtime/launches/${launch.launchId}/config`, route => route.fulfill({json: config}));
    await context.route(new URL(game.url, base).href, route => route.fulfill({status: 200, body: bytes,
      contentType: "application/octet-stream", headers: {"content-length": String(sizeBytes), etag: `"sha256-${game.sha256}"`}}));
    const network = observeContentIO(context, contentSourceMatcher(computerResources(config), base)); await network.ready;
    const page = await context.newPage(), workers = [], closed = new Set();
    page.on("worker", worker => {workers.push(worker); worker.on("close", () => closed.add(worker));});
    let paused, release, commit;
    if (sizeBytes === maximum) {
      const reached = new Promise(resolve => {paused = resolve;});
      const resume = new Promise(resolve => {release = resolve;});
      commit = await pauseContentCommit(context, page, provider.files.candidate.get("assets/content-io/worker.mjs").toString(),
        async values => {paused(values); await resume; return {boundary: true};});
      await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded"});
      let timer;
      const materialized = await Promise.race([reached, new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error("COMPUTER_BOUNDARY_NOT_MATERIALIZED")), 60000);
      })]).finally(() => clearTimeout(timer));
      assert.equal(materialized.size, sizeBytes); assert.equal(materialized.written, sizeBytes);
      for (const frame of page.frames()) assert.equal(await frame.locator("canvas").count(), 0);
      release();
      // Wait for the actual materialization to finish, then observe the native
      // canvas creation; a valid game scene is intentionally not asserted here.
      const deadline = Date.now() + 30000;
      let canvases = 0;
      while (!canvases && Date.now() < deadline) {
        for (const frame of page.frames()) canvases += await frame.locator("canvas").count();
        if (!canvases) await page.waitForTimeout(50);
      }
      assert.ok(canvases > 0, "COMPUTER_BOUNDARY_CORE_NOT_STARTED");
      report.maximum = {materialized, injection: await commit.finish(), canvasCount: canvases};
    } else {
      await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded"});
      // SAM rejects its native disk limit before Session.open; the Provider's
      // public error mapper intentionally reports this adapter error generically.
      const error = target === "samcoupe" ? "RUNTIME_FAILED" : "CONTENT_IO_SOURCE_INVALID";
      await page.getByText(error, {exact: false}).first().waitFor({timeout: 30000});
      for (const frame of page.frames()) assert.equal(await frame.locator("canvas").count(), 0);
      report.rejected = {errorCode: error, canvasCount: 0};
    }
    await network.flush();
    const gameRequests = network.requests.filter(row => row.path === new URL(game.url, base).pathname);
    if (sizeBytes === maximum) {
      assert.equal(gameRequests.length, 1); assert.equal(gameRequests[0].sizeBytes, maximum);
    } else assert.equal(gameRequests.length, 0, "COMPUTER_OVERSIZE_GAME_FETCHED");
    await page.screenshot({path: join(directory, `${sizeBytes}.png`)});
    // Product capabilities have no review /finish endpoint. These admission
    // probes close their owned page; ordinary Player exits have separate proof.
    await page.close();
    assert.equal(closed.size, workers.length, "COMPUTER_BOUNDARY_WORKER_LEAK");
    report.inputs.push({sizeBytes, sha256: game.sha256, launchId: launch.launchId,
      runtime: {bundleSha256: config.runtime.bundleSha256, moduleSha256: config.runtime.moduleSha256},
      requests: network.requests, sessions: collector.snapshot(page), cleanup: "OWNED_PAGE_CLOSE",
      workersCreated: workers.length, workersClosed: closed.size});
    network.close(); await context.close();
  }
  report.status = "PASS";
} catch (error) {
  report.errorCode = error.message; report.stack = error.stack; process.exitCode = 1;
  report.observedErrors = [];
  for (const context of browser?.contexts() ?? []) for (const page of context.pages()) {
    const body = await page.locator("body").innerText().catch(() => "");
    report.observedErrors.push(...body.match(/\b[A-Z][A-Z_]+(?:INVALID|FAILED|ERROR|UNAVAILABLE)\b/gu) ?? []);
    await page.screenshot({path: join(directory, "boundary-failure.png")}).catch(() => {});
  }
}
finally {
  await browser?.close(); await proxy?.close();
  await writeFile(join(directory, "boundary-product.json"), JSON.stringify(report, null, 2) + "\n");
  console.log(JSON.stringify({caseId: report.caseId, status: report.status, errorCode: report.errorCode}));
}
