import assert from "node:assert/strict";
import {mkdir, readFile, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {pathToFileURL} from "node:url";
import {chromium, expect} from "../../web/node_modules/@playwright/test/index.mjs";
import {fantasyClient, launchCart} from "./fantasy_product_client.mjs";
import {emulatorjsSlowProxy} from "./emulatorjs_slow_proxy.mjs";
import {revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";

export async function verifyEmulatorJsStartupFailures({browser, base, input, screenshots}) {
  await mkdir(screenshots, {recursive: true});
  const proxy = await emulatorjsSlowProxy(base); proxy.stall(true);
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  const report = {}, failures = []; let page;
  try {
    const client = await fantasyClient(context, base); page = await context.newPage();
    const session = await context.newCDPSession(page);
    await session.send("Network.enable");
    // A reload otherwise resumes a partially cached immutable response using
    // Range probes. Exercise live transport independently of the HTTP cache.
    await session.send("Network.setCacheDisabled", {cacheDisabled: true});
    page.on("requestfailed", request => {
      if (new URL(request.url()).pathname.endsWith(".data")) {
        failures.push({path: new URL(request.url()).pathname, error: request.failure()?.errorText});
      }
    });
    const launch = await launchCart(client, input.gameId);
    await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded"});
    await expect(page.getByText("资源下载已停止推进，请检查网络后重试。", {exact: true})).toBeVisible({timeout: 40000});
    await expect(page.getByRole("button", {name: "重试启动"})).toBeVisible();
    for (const [name, width, height, deviceScaleFactor] of [["phone", 390, 844, 1], ["4k", 2560, 1440, 1.5]]) {
      await session.send("Emulation.setDeviceMetricsOverride", {width, height, deviceScaleFactor, mobile: false});
      await expect.poll(() => page.evaluate(() => [innerWidth, innerHeight, devicePixelRatio])).toEqual([width, height, deviceScaleFactor]);
      const capture = await session.send("Page.captureScreenshot", {format: "png", fromSurface: true, captureBeyondViewport: false});
      const bytes = Buffer.from(capture.data, "base64");
      assert.deepEqual([bytes.readUInt32BE(16), bytes.readUInt32BE(20)], [width * deviceScaleFactor, height * deviceScaleFactor]);
      await writeFile(join(screenshots, `${name}-startup-idle.png`), bytes);
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    }
    report.idleFailure = true;
    await page.getByRole("button", {name: "重试启动"}).click();
    await expect.poll(() => proxy.requests.length, {timeout: 10000}).toBeGreaterThan(1);
    await expect.poll(() => proxy.requests[1].bytes, {timeout: 10000}).toBeGreaterThan(0);
    // Wait for actual browser byte progress before deliberately breaking the
    // response, rather than a server write still buffered in the transport.
    await expect(page.frameLocator("iframe.player-frame").getByText(/下载游戏核心 \d+%/u)).toBeVisible({timeout: 10000});
    assert.ok(!proxy.requests[1].closed, "RETRY_TRANSFER_ALREADY_CLOSED");
    report.retryRequested = true; proxy.disconnect();
    await expect(page.getByText("资源下载失败，请检查网络后重试。", {exact: true})).toBeVisible({timeout: 5000});
    report.networkFailure = true;
    const third = await launchCart(client, input.gameId);
    await page.goto(base + third.playUrl, {waitUntil: "domcontentloaded"});
    await expect.poll(() => proxy.requests.length, {timeout: 10000}).toBeGreaterThan(2);
    const started = performance.now(); await revealPreviewToolbar(page);
    await page.getByRole("button", {name: "返回并退出游戏", exact: true}).click();
    await page.getByRole("alertdialog", {name: "退出游戏？"}).getByRole("button", {name: "退出游戏", exact: true}).click();
    await page.waitForURL(base + `/games/${input.gameId}`);
    report.cancelMs = performance.now() - started; assert.ok(report.cancelMs < 5000);
    await expect.poll(() => proxy.requests.every(row => row.closed), {timeout: 5000}).toBe(true);
    assert.ok(proxy.requests.every(row => !row.complete), "FAILED_CORE_CACHED_AS_COMPLETE");
    await session.detach();
    report.failedCoreRequests = proxy.requests;
    return report;
  } catch (error) {
    const visible = await page?.locator(".player-loading").innerText().catch(() => "");
    throw new Error(`${error.message}\nvisible startup state: ${visible ?? ""}\ntransfers: ${JSON.stringify(proxy.requests)}\nrequest failures: ${JSON.stringify(failures)}`, {cause: error});
  } finally {await context.close(); await proxy.close();}
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const env = process.env;
  assert.ok(env.RETROM_ACCEPTANCE_BASE_URL && env.RETROM_EJS_SLOW_INPUT && env.RETROM_CHROME_EXECUTABLE, "EJS_SLOW_INPUT_REQUIRED");
  const input = JSON.parse(await readFile(env.RETROM_EJS_SLOW_INPUT, "utf8"));
  const browser = await chromium.launch({headless: true, executablePath: env.RETROM_CHROME_EXECUTABLE,
    args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  try {
    const result = await verifyEmulatorJsStartupFailures({browser, base: env.RETROM_ACCEPTANCE_BASE_URL,
      input: input.fbneo, screenshots: resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/emulatorjs-slow", "screenshots/regression")});
    console.log(JSON.stringify({status: "PASS", ...result}));
  } finally {await browser.close();}
}
