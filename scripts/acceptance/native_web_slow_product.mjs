import assert from "node:assert/strict";
import {readFile, mkdir, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {createHash} from "node:crypto";
import {chromium, expect} from "../../web/node_modules/@playwright/test/index.mjs";
import {fantasyClient, launchCart} from "./fantasy_product_client.mjs";
import {nativeWebSlowProxy} from "./native_web_slow_proxy.mjs";
import {nativeActions, nativeSnapshot, chooseNativeLoading} from "./native_web_cache_browser.mjs";
import {assertNativeInput} from "./native_web_cache_contract.mjs";
import {exitContentIOPlayer} from "./content_io_player_exit.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const output = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/native-web-slow");
await mkdir(output, {recursive: true});
const report = {schemaVersion: 1, caseId: "ACC-CONTENT-004", status: "FAIL", phases: []};
let browser;
async function flush() {
  await writeFile(join(output, "native-web-slow-product.json"), JSON.stringify(report, null, 2) + "\n");
}
try {
  assert.ok(base && env.RETROM_CHROME_EXECUTABLE && env.RETROM_NATIVE_CACHE_INPUT, "NATIVE_SLOW_INPUT_REQUIRED");
  const input = await readFile(env.RETROM_NATIVE_CACHE_INPUT, "utf8"), plan = JSON.parse(input);
  report.inputSha256 = createHash("sha256").update(input).digest("hex");
  assert.ok(["rpgmaker-mv", "rpgmaker-mz"].includes(plan.targetId), "NATIVE_SLOW_TARGET_INVALID");
  report.targetId = plan.targetId; report.gameId = plan.gameId;
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--autoplay-policy=no-user-gesture-required"]});
  await phase(plan, "ON_DEMAND"); await phase(plan, "PRELOAD"); report.status = "PASS";
} catch (error) {report.error = error.message; process.exitCode = 1;}
finally {
  await browser?.close();
  await flush();
  console.log(JSON.stringify({caseId: report.caseId, targetId: report.targetId, status: report.status, error: report.error}));
}
async function phase(plan, mode) {
  const proxy = await nativeWebSlowProxy(base), context = await browser.newContext({viewport: {width: 1440, height: 1000}, ...proxy.contextOptions});
  const result = {mode, bytesPerSecond: proxy.bytesPerSecond, latencyMs: proxy.latencyMs, requests: proxy.requests};
  report.phases.push(result);
  let page;
  try {
    await context.route("**/*", route => route.continue());
    const client = await fantasyClient(context, base);
    await chooseNativeLoading(context, base, plan.gameId, mode);
    const launch = await launchCart(client, plan.gameId); launch.returnTo = `/games/${plan.gameId}`;
    result.launchId = launch.launchId;
    page = await context.newPage(); const started = performance.now();
    await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded"});
    if (mode === "ON_DEMAND") {
      await expect.poll(async () => {
        const loading = (await page.locator(".player-loading").allTextContents()).join(" ");
        assert.ok(!/超时|下载未完成|RPG_[A-Z_]+|CONTENT_IO_[A-Z_]+|Aborted/u.test(loading), `NATIVE_SLOW_FAILURE: ${loading}`);
        return await page.locator(".player-loading").count() === 0 && !!await nativeSnapshot(page);
      }, {timeout: 180000, intervals: [500]}).toBe(true);
      result.readyMs = performance.now() - started;
      result.ready = await nativeSnapshot(page); await flush();
      assert.ok(result.readyMs > 10000, "NATIVE_SLOW_DID_NOT_EXERCISE_STARTUP_DEADLINE");
      await nativeActions(page, plan.start); result.before = await nativeSnapshot(page);
      await nativeActions(page, plan.advance); result.after = await nativeSnapshot(page);
      assertNativeInput(result.before, result.after);
    } else {
      await expect.poll(() => proxy.requests.some(row => row.path.includes("/files/") && row.bytes > 0), {timeout: 30000}).toBe(true);
      await page.waitForTimeout(Math.max(0, 12000 - (performance.now() - started)));
      result.loading = (await page.locator(".player-loading").allTextContents()).join(" ");
      assert.match(result.loading, /MiB|KiB|B\s*\//u);
      assert.ok(!/未完成|失败|TIMEOUT/u.test(result.loading));
      assert.equal(await nativeSnapshot(page), null, "NATIVE_SLOW_ENGINE_STARTED_BEFORE_PRELOAD");
    }
    await page.screenshot({path: join(output, `${mode}.png`)});
    const exiting = performance.now();
    await exitContentIOPlayer(page, base, launch, "GAME_SAVE");
    result.exitMs = performance.now() - exiting;
    assert.ok(result.exitMs < 5000, "NATIVE_SLOW_EXIT_WAITED_FOR_DOWNLOAD");
    assert.ok(proxy.requests.some(row => row.complete && row.bytes >= 262144 && row.elapsedMs >= row.bytes / proxy.bytesPerSecond * 1000), "NATIVE_SLOW_THROTTLE_NOT_OBSERVED");
    result.exited = true;
  } catch (error) {
    result.failure = error.message;
    result.snapshot = page ? await nativeSnapshot(page).catch(() => null) : null;
    await page?.screenshot({path: join(output, `${mode}-failure.png`)}).catch(() => {});
    throw error;
  } finally {await context.close(); await proxy.close();}
}
