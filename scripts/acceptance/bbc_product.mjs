import assert from "node:assert/strict";
import {mkdir, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {randomUUID} from "node:crypto";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, approveCart, launchCart, saveCart} from "./fantasy_product_client.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {computerSource, installComputerBios, importComputer} from "./computer_product_client.mjs";
import {openComputer, pictureComputer, pauseComputer, closeComputer} from "./computer_product_browser.mjs";
import {observeBBC, bootBatBall, moveBatBall} from "./bbc_product_browser.mjs";
import {completeComputerContentProof} from "./computer_content_proof.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/bbc-product");
await mkdir(directory, {recursive: true});
const report = {schemaVersion: 1, caseId: "ACC-BBC-001", runId: env.RETROM_CONTENT_IO_RUN_ID ?? randomUUID(),
  status: "FAIL", scope: "PRODUCT_LIFECYCLE", launches: []};
let browser, proxy, active;
const stage = async name => {
  report.stage = name; console.log(name);
  await writeFile(join(directory, "bbc-product.json"), JSON.stringify(report, null, 2) + "\n");
};
try {
  assert.ok(base && env.RETROM_CHROME_EXECUTABLE && env.RETROM_BBC_DISK && env.RETROM_BBC_BIOS, "BBC_OPERATOR_INPUT_REQUIRED");
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(15000);
  await installVirtualStandardGamepad(context); await observeBBC(context);
  const collector = await observeContentStoreEvents(context, {retain: true}), client = await fantasyClient(context, base);
  const bios = await installComputerBios(client, "jsbeeb", JSON.parse(env.RETROM_BBC_BIOS));
  report.sources = [await computerSource(env.RETROM_BBC_DISK), ...bios];
  const review = await importComputer(client, "bbc", "jsbeeb", env.RETROM_BBC_DISK);
  report.import = {itemId: review.itemId, importJobId: review.importJobId};
  await stage("import");
  const preview = await openComputer(context, base, await previewCart(client, review.itemId), "bbc-jsbeeb", report.sources);
  active = preview; await stage("preview-open");
  report.preview = {initial: await bootBatBall(preview), input: await moveBatBall(preview), screenshot: await pictureComputer(preview, directory, "preview")};
  report.launches.push(await closeComputer(preview, base, collector));
  await stage("preview");
  const {gameId} = await approveCart(client, review.itemId); report.gameId = gameId;
  const original = await launchCart(client, gameId); original.returnTo = `/games/${gameId}`;
  const opened = await openComputer(context, base, original, "bbc-jsbeeb", report.sources);
  active = opened; await stage("product-open");
  report.initial = await bootBatBall(opened); report.input = await moveBatBall(opened);
  await pauseComputer(opened); report.savedScreenshot = await pictureComputer(opened, directory, "saved");
  const saved = await saveCart(opened.page, original.launchId, "jsbeeb");
  report.save = saved;
  report.nativeSave = await opened.frame.evaluate(() => globalThis.__bbcProduct.captures.at(-1));
  await stage("save");
  report.launches.push(await closeComputer(opened, base, collector));
  const next = await launchCart(client, gameId, saved.saveStateId); next.returnTo = `/games/${gameId}`;
  assert.notEqual(next.launchId, original.launchId);
  const restored = await openComputer(context, base, next, "bbc-jsbeeb", report.sources);
  active = restored; await stage("restore-open");
  report.nativeRestore = await restored.frame.evaluate(() => globalThis.__bbcProduct.restores.at(-1));
  assert.deepEqual(report.nativeRestore, report.nativeSave, "BBC_NATIVE_RESTORE_MISMATCH");
  report.restoredScreenshot = await pictureComputer(restored, directory, "restored");
  report.restoredInput = await moveBatBall(restored, 14);
  await restored.network.flush(); assert.equal(restored.network.requests.length, 0, "BBC_WARM_CONTENT_NETWORK");
  report.launches.push(await closeComputer(restored, base, collector));
  assert.ok(report.launches[0].requests.filter(row => row.method === "GET").length === 4, "BBC_COLD_GAME_BIOS_MISSING");
  report.status = "PASS";
} catch (error) {
  report.errorCode = error.message; report.stack = error.stack; process.exitCode = 1;
  await active?.page.screenshot({path: join(directory, "failure.png")}).catch(() => {});
}
finally {
  await browser?.close(); await proxy?.close();
  await writeFile(join(directory, "bbc-product.json"), JSON.stringify(report, null, 2) + "\n");
  console.log(JSON.stringify({caseId: report.caseId, status: report.status, errorCode: report.errorCode}));
}
if (report.status === "PASS" && env.RETROM_CONTENT_IO_FULL_PROOF === "1") await completeComputerContentProof(directory, report, "bbc-jsbeeb");
