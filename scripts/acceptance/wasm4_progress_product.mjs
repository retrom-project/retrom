import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {mkdir, readFile, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {fantasyClient} from "./fantasy_product_client.mjs";
import {measureWasm4Launch} from "./wasm4_performance_browser.mjs";
import {pauseContentCommit} from "./content_io_commit_pause.mjs";
import {validateContentResources} from "./content_io_performance.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL, directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR);
const input = JSON.parse(await readFile(env.RETROM_CONTENT_IO_PROGRESS_INPUT, "utf8"));
await mkdir(directory, {recursive: false});
const report = {schemaVersion: 1, caseId: "ACC-WASM4-001", runId: env.RETROM_CONTENT_IO_RUN_ID ?? null, scenario: "eager-progress", status: "FAIL"};
let browser, proxy, pause;
try {
  const workerSource = await readFile(input.workerPath, "utf8");
  assert.equal(createHash("sha256").update(workerSource).digest("hex"), input.workerSha256);
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await installVirtualStandardGamepad(context);
  const collector = await observeContentStoreEvents(context, {retain: true}), client = await fantasyClient(context, base);
  report.launch = await measureWasm4Launch({browser, context, collector, client, base, gameId: input.gameId,
    variant: "candidate", source: input.source, directory, preparePage: async page => {
      pause = await pauseContentCommit(context, page, workerSource, async values => {
        assert.equal(values.kind, "BYTES"); assert.equal(values.size, input.source.sizeBytes);
        const progress = page.getByRole("progressbar"); await progress.waitFor({state: "visible", timeout: 5000});
        const percentage = Number(await progress.getAttribute("aria-valuenow"));
        assert.ok(Number.isFinite(percentage) && percentage >= 0 && percentage < 100, "CONTENT_IO_PREMATURE_COMPLETE_PROGRESS");
        assert.equal(await page.locator(".player-loading").isVisible(), true);
        let canvasCount = 0; for (const frame of page.frames()) canvasCount += await frame.locator("canvas").count();
        assert.equal(canvasCount, 0, "CONTENT_IO_CORE_STARTED_BEFORE_COMMIT");
        await page.screenshot({path: join(directory, "before-commit.png")});
        return {percentage, loadingVisible: true, canvasCount};
      });
    }});
  report.commit = await pause.finish();
  assert.equal(report.launch.runtime.bundleSha256, input.bundleSha256); assert.equal(report.launch.runtime.moduleSha256, input.moduleSha256);
  validateContentResources(report.launch.metrics.publicPeak, false); validateContentResources(report.launch.metrics.closed, true);
  report.status = "PASS";
} catch (error) {report.errorCode = error.message; report.stack = error.stack; report.pauseState = pause?.snapshot(); process.exitCode = 1;}
finally {await browser?.close(); await proxy?.close(); await writeFile(join(directory, "progress-product.json"), JSON.stringify(report, null, 2) + "\n");}
