import assert from "node:assert/strict";
import {mkdir, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {chromium, expect} from "../../web/node_modules/@playwright/test/index.mjs";
import sharp from "../../web/node_modules/sharp/dist/index.cjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {fantasyClient, previewCart, approveCart, launchCart, saveCart} from "./fantasy_product_client.mjs";
import {importComputer} from "./computer_product_client.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {closeComputer, pauseComputer} from "./computer_product_browser.mjs";
import {openDOS, observeDOSStates} from "./dosbox_product_browser.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/content-preload-product");
const screenshots = join(directory, "screenshots");
await mkdir(screenshots, {recursive: true});
const report = {schemaVersion: 1, caseId: "ACC-CONTENT-001", status: "FAIL", launches: []};
let browser, proxy, active, cleanupClient, cleanupContext;
const stage = async name => {
  report.stage = name; console.log(name);
  await writeFile(join(directory, "content-preload-product.json"), JSON.stringify(report, null, 2) + "\n");
};

async function color(opened) {
  const png = await opened.canvas.screenshot({animations: "disabled"});
  const bytes = await sharp(png).resize(1, 1).removeAlpha().raw().toBuffer();
  return Array.from(bytes, channel => channel > 80 ? 1 : 0);
}

async function press(opened, expected) {
  await opened.canvas.click(); await opened.page.keyboard.press("Space");
  await expect.poll(() => color(opened), {timeout: 15000}).toEqual(expected);
  return color(opened);
}

try {
  assert.ok(base && env.RETROM_CHROME_EXECUTABLE, "CONTENT_PRELOAD_ENV_REQUIRED");
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const options = {viewport: {width: 2560, height: 1440}, deviceScaleFactor: 1.5, ...proxy.contextOptions};
  const previewContext = await browser.newContext(options), previewClient = await fantasyClient(previewContext, base);
  cleanupClient = previewClient; cleanupContext = previewContext;
  const previewCollector = await observeContentStoreEvents(previewContext, {retain: true});
  const review = await importComputer(previewClient, "dos", "dosbox_pure", resolve("testdata/public-roms/dos-cache/dos-cache.zip"));
  report.import = {itemId: review.itemId, importJobId: review.importJobId};
  const snapshot = await previewClient.raw("GET", `/api/v1/admin/reviews/${review.itemId}`);
  await previewClient.json("PATCH", `/api/v1/admin/reviews/${review.itemId}`, {
    headers: {...previewClient.writeHeaders(), "If-Match": snapshot.headers().etag}, data: {defaultDosEntry: "CACHE.COM", tagIds: []},
  });
  await stage("review-preview");
  const preview = active = await openDOS(previewContext, base, await previewCart(previewClient, review.itemId));
  await expect.poll(() => color(preview), {timeout: 20000}).toEqual([0, 0, 1]);
  await preview.network.flush(); report.onDemand = preview.rangeSummary();
  assert.ok(report.onDemand.downloadedBytes < preview.source.sizeBytes, "DEFAULT_MUST_REMAIN_ON_DEMAND");
  report.previewInput = await press(preview, [0, 1, 0]);
  report.launches.push(await closeComputer(preview, base, previewCollector));
  report.gameId = (await approveCart(previewClient, review.itemId)).gameId;
  await previewContext.close();

  // A fresh browser context prevents the review's partial cache from masking a cold preload.
  const context = await browser.newContext(options), client = await fantasyClient(context, base);
  cleanupClient = client; cleanupContext = context;
  context.setDefaultTimeout(20000); await observeDOSStates(context);
  const collector = await observeContentStoreEvents(context, {retain: true});
  const details = await context.newPage();
  await details.goto(`${base}/games/${report.gameId}`);
  const choice = details.getByRole("combobox", {name: "内容加载", exact: true});
  await expect(choice).toHaveValue("ON_DEMAND"); await choice.selectOption("PRELOAD");
  await details.reload(); await expect(choice).toHaveValue("PRELOAD");
  await details.screenshot({path: join(screenshots, "detail-4k-150.png"), fullPage: true});
  await details.setViewportSize({width: 390, height: 844});
  await details.getByRole("button", {name: "启动选项", exact: true}).click();
  await expect(details.getByRole("combobox", {name: "内容加载", exact: true})).toHaveValue("PRELOAD");
  await details.screenshot({path: join(screenshots, "detail-mobile-options.png"), fullPage: true});
  await details.close(); await stage("preload-offline");
  const launch = await launchCart(client, report.gameId, null, "CACHE.COM"); launch.returnTo = `/games/${report.gameId}`;
  const opened = active = await openDOS(context, base, launch);
  await expect.poll(() => color(opened), {timeout: 20000}).toEqual([0, 0, 1]);
  await opened.network.flush();
  const downloaded = opened.network.requests;
  assert.ok(downloaded.length > 1);
  assert.ok(downloaded.every(row => row.status === 206 && row.failure === null && row.range !== null));
  assert.equal(downloaded.reduce((sum, row) => sum + row.sizeBytes, 0), opened.source.sizeBytes, "PRELOAD_MUST_COVER_COMPLETE_FILE");
  report.preload = {sizeBytes: opened.source.sizeBytes, requests: downloaded.length};
  const requestsBeforeInput = downloaded.length;
  await context.setOffline(true);
  report.offlineInput = await press(opened, [0, 1, 0]);
  await opened.page.screenshot({path: join(screenshots, "offline-input.png")});
  assert.equal(opened.network.requests.length, requestsBeforeInput, "OFFLINE_INPUT_MUST_USE_LOCAL_BYTES");
  await context.setOffline(false); await pauseComputer(opened);
  report.save = await saveCart(opened.page, launch.launchId, "dosbox_pure");
  report.nativeSave = await opened.frame.evaluate(async () => {
    await Promise.all(globalThis.__dosStateObservation.pending); return globalThis.__dosStateObservation.captures.at(-1);
  });
  assert.ok(report.nativeSave?.sizeBytes > 0);
  report.launches.push(await closeComputer(opened, base, collector)); await stage("restore-offline");
  const next = await launchCart(client, report.gameId, report.save.saveStateId); next.returnTo = launch.returnTo;
  assert.notEqual(next.launchId, launch.launchId);
  const restored = active = await openDOS(context, base, next);
  await expect.poll(() => color(restored), {timeout: 20000}).toEqual([0, 1, 0]);
  report.nativeRestore = await restored.frame.evaluate(() => globalThis.__dosStateObservation.restores.at(-1));
  assert.deepEqual(report.nativeRestore, report.nativeSave);
  await context.setOffline(true); report.restoredInput = await press(restored, [0, 1, 1]);
  await context.setOffline(false); await restored.network.flush();
  assert.equal(restored.network.requests.length, 0, "REPEAT_LAUNCH_MUST_REUSE_COMPLETE_CACHE");
  report.launches.push(await closeComputer(restored, base, collector)); report.status = "PASS";
} catch (error) {
  report.error = error.message; process.exitCode = 1;
  await active?.page.screenshot({path: join(screenshots, "failure.png")}).catch(() => {});
} finally {
  if (report.gameId && cleanupClient) {
    try {
      await cleanupContext.setOffline(false);
      const path = `/api/v1/admin/games/${report.gameId}`;
      const response = await cleanupClient.raw("GET", path), game = await response.json();
      assert.equal(game.title, "dos-cache");
      await cleanupClient.json("DELETE", path, {expected: 202, headers: {...cleanupClient.writeHeaders(), "If-Match": response.headers().etag},
        data: {confirmTitle: game.title, impactDigest: game.deleteImpact.impactDigest}});
      report.fixtureRemoved = true;
    } catch (error) {report.cleanupError = error.message; report.status = "FAIL"; process.exitCode = 1;}
  }
  await browser?.close(); await proxy?.close(); await stage(report.status);
  console.log(JSON.stringify({caseId: report.caseId, status: report.status, error: report.error}));
}
