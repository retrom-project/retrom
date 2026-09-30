import assert from "node:assert/strict";
import {mkdir, readFile, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {chromium, expect} from "../../web/node_modules/@playwright/test/index.mjs";
import sharp from "../../web/node_modules/sharp/dist/index.cjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {fantasyClient, importCart, previewCart, approveCart, launchCart, runtimeCanvas, gamepad, saveCart} from "./fantasy_product_client.mjs";
import {withFantasyRunCart} from "./fantasy_run_cart.mjs";
import {spriteState} from "./fantasy_fixture.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {finishPreview} from "./rpgmaker_preview_actions.mjs";
import {exitContentIOPlayer} from "./content_io_player_exit.mjs";
import {observeContentIO, contentSourceMatcher} from "./content_io_observation.mjs";
import {denyContentWorkerStorage} from "./content_io_storage_denial.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/content-loading-capability");
await mkdir(directory, {recursive: true});
const report = {schemaVersion: 1, caseId: "ACC-CONTENT-003", status: "FAIL", displays: [], launches: []};
let browser, proxy, context, client, plan;
const options = {viewport: {width: 2560, height: 1440}, deviceScaleFactor: 1.5};
async function flush() {await writeFile(join(directory, "content-loading-capability-product.json"), JSON.stringify(report, null, 2) + "\n");}
async function startContext() {
  await context?.close();
  context = await browser.newContext({...options, ...proxy.contextOptions});
  context.setDefaultTimeout(20000);
  await context.route("**/*", route => route.continue());
  await installVirtualStandardGamepad(context);
  client = await fantasyClient(context, base);
}
async function screenshot(page, name, physical4k = false) {
  await mkdir(join(directory, "screenshots"), {recursive: true});
  const path = join(directory, "screenshots", `${name}.png`);
  await page.screenshot({path});
  const metadata = await sharp(path).metadata();
  if (physical4k) {
    assert.equal(await page.evaluate(() => devicePixelRatio), 1.5);
    assert.deepEqual([metadata.width, metadata.height], [3840, 2160]);
  }
  return {path: `screenshots/${name}.png`, width: metadata.width, height: metadata.height};
}
async function choose(mode) {
  const page = await context.newPage();
  await page.goto(`${base}/games/${plan.dualGameId}`);
  const choice = page.getByRole("combobox", {name: "内容加载", exact: true});
  await choice.selectOption(mode); await expect(choice).toHaveValue(mode);
  await page.reload(); await expect(choice).toHaveValue(mode);
  await page.close();
}
async function display(gameId, capability, label) {
  const detail = await client.json("GET", `/api/v1/games/${gameId}`);
  const core = detail.coreOptions.find(core => core.isDefault);
  assert.equal(core.contentLoading, capability);
  const page = await context.newPage();
  await page.goto(`${base}/games/${gameId}`);
  async function check() {
    const choice = page.getByRole("combobox", {name: "内容加载", exact: true});
    if (capability) {
      await expect(choice).toHaveValue(capability === "PRELOAD_ONLY" ? "PRELOAD" : "ON_DEMAND");
      await expect(choice.locator("option")).toHaveCount(capability === "PRELOAD_ONLY" ? 1 : 2);
      await expect(choice).toBeEnabled();
      if (page.viewportSize().width >= 1600) {
        await expect.poll(async () => {
          const select = await choice.boundingBox(), last = await page.locator(".launch-actions button").last().boundingBox();
          return Math.abs(select.x + select.width - last.x - last.width);
        }).toBeLessThan(1);
      }
    } else {
      await expect(choice).toHaveCount(0);
      await expect(page.getByText("内容加载", {exact: true})).toHaveCount(0);
    }
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  }
  await check();
  const desktop = await screenshot(page, `${label}-4k`, true);
  await page.setViewportSize({width: 390, height: 844});
  await page.getByRole("button", {name: "启动选项", exact: true}).click();
  await check();
  const mobile = await screenshot(page, `${label}-mobile`);
  report.displays.push({gameId, capability, desktop, mobile}); await page.close(); await flush();
}
async function publishOwnedCart() {
  return withFantasyRunCart("fake08", resolve("testdata/public-roms/fantasy-controls/controls.p8"), async filename => {
    const review = await importCart(client, "fake08", filename);
    const preview = await previewCart(client, review.itemId);
    const page = await context.newPage(); await page.goto(`${base}${preview.playUrl}`);
    const canvas = await runtimeCanvas(page, "fake08");
    assert.equal((await spriteState(canvas)).x, 20);
    await finishPreview(page, preview.previewId);
    return (await approveCart(client, review.itemId)).gameId;
  });
}
async function verifyFixedLoading() {
  const open = async saveId => {
    const launch = await launchCart(client, report.fullGameId, saveId); launch.returnTo = `/games/${report.fullGameId}`;
    const config = await client.json("GET", `/runtime/launches/${launch.launchId}/config`);
    assert.equal(config.runtime.capabilities.contentLoading, "PRELOAD_ONLY");
    const source = config.resources.find(resource => resource.role === "game");
    const network = observeContentIO(context, contentSourceMatcher([source], base)); await network.ready;
    const page = await context.newPage(); await page.goto(`${base}${launch.playUrl}`);
    const canvas = await runtimeCanvas(page, "fake08");
    return {launch, page, canvas, source, network};
  };
  const first = await open(null);
  const initial = await spriteState(first.canvas);
  await gamepad(first.page, 15); const moved = await spriteState(first.canvas);
  assert.ok(moved.x > initial.x);
  await first.network.flush();
  assert.equal(first.network.requests.reduce((sum, row) => sum + row.sizeBytes, 0), first.source.sizeBytes);
  report.preload = {bytes: first.source.sizeBytes, inputBefore: initial, inputAfter: moved};
  const save = await saveCart(first.page, first.launch.launchId, "fake08");
  report.launches.push(first.launch.launchId); first.network.close();
  await exitContentIOPlayer(first.page, base, first.launch); await first.page.close();
  const restored = await open(save.saveStateId);
  assert.deepEqual(await spriteState(restored.canvas), moved);
  await gamepad(restored.page, 14); assert.ok((await spriteState(restored.canvas)).x < moved.x);
  await restored.network.flush(); assert.equal(restored.network.requests.length, 0);
  report.restore = {saveStateId: save.saveStateId, warmRequests: 0}; report.launches.push(restored.launch.launchId);
  restored.network.close(); await exitContentIOPlayer(restored.page, base, restored.launch); await restored.page.close();
}
async function verifyStorageFailure(gameId, canLoadOnDemand) {
  await startContext(); await choose(canLoadOnDemand ? "PRELOAD" : "ON_DEMAND");
  const launch = await launchCart(client, gameId), page = await context.newPage();
  const fault = await denyContentWorkerStorage(context, page, {workerName: "retrom-content-preload"});
  try {
    await page.goto(`${base}${launch.playUrl}`);
    await expect(page.getByRole("button", {name: "重试下载", exact: true})).toBeVisible({timeout: 30000});
    await expect(page.getByRole("button", {name: "改为按需加载", exact: true})).toHaveCount(canLoadOnDemand ? 1 : 0);
    report[canLoadOnDemand ? "dualFailure" : "fixedFailure"] = {launchId: launch.launchId,
      screenshot: await screenshot(page, canLoadOnDemand ? "dual-failure" : "fixed-failure")};
  } catch (error) {
    report.failurePage = {text: await page.locator(".player-loading").innerText().catch(() => "unavailable"),
      screenshot: await screenshot(page, "storage-failure-observed")};
    throw error;
  } finally {
    report.storageFault = await fault.finish();
    await page.close();
  }
  await flush();
}
try {
  assert.ok(base && env.RETROM_CHROME_EXECUTABLE && env.RETROM_LOADING_CAPABILITY_INPUT, "LOADING_CAPABILITY_INPUT_REQUIRED");
  plan = JSON.parse(await readFile(env.RETROM_LOADING_CAPABILITY_INPUT, "utf8"));
  for (const gameId of [plan.dualGameId, plan.unmanagedGameId]) assert.match(gameId, /^[0-9a-f-]{36}$/u);
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--autoplay-policy=no-user-gesture-required"]});
  await startContext(); report.fullGameId = await publishOwnedCart();
  await startContext(); await choose("ON_DEMAND");
  await display(plan.dualGameId, "ON_DEMAND_AND_PRELOAD", "dual");
  await display(report.fullGameId, "PRELOAD_ONLY", "fixed");
  await display(plan.unmanagedGameId, null, "unmanaged");
  await verifyFixedLoading();
  await verifyStorageFailure(report.fullGameId, false);
  await verifyStorageFailure(plan.dualGameId, true);
  report.status = "PASS";
} catch (error) {report.error = error.message; process.exitCode = 1;}
finally {
  if (report.fullGameId && client) {
    try {
      const path = `/api/v1/admin/games/${report.fullGameId}`, response = await client.raw("GET", path), game = await response.json();
      await client.json("DELETE", path, {expected: 202, headers: {...client.writeHeaders(), "If-Match": response.headers().etag},
        data: {confirmTitle: game.title, impactDigest: game.deleteImpact.impactDigest}});
      report.fixtureRemoved = true;
    } catch (error) {report.cleanupError = error.message; report.status = "FAIL"; process.exitCode = 1;}
  }
  await browser?.close(); await proxy?.close(); await flush();
  console.log(JSON.stringify({caseId: report.caseId, status: report.status, error: report.error, cleanupError: report.cleanupError}));
}
