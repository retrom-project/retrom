import assert from "node:assert/strict";
import {mkdir, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {randomUUID} from "node:crypto";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, approveCart, launchCart, saveCart} from "./fantasy_product_client.mjs";
import {computerSource, importComputer} from "./computer_product_client.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {pictureComputer, pauseComputer, closeComputer} from "./computer_product_browser.mjs";
import {openDOS, observeDOSStates} from "./dosbox_product_browser.mjs";
import {completeDOSContentProof} from "./dosbox_content_proof.mjs";
import {bootDoom, moveAndFireDoom, doomScene} from "./dosbox_doom_actions.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/dosbox-product"); await mkdir(directory, {recursive: true});
const report = {schemaVersion: 1, caseId: "ACC-DOSBOX-001", runId: env.RETROM_CONTENT_IO_RUN_ID ?? randomUUID(),
  status: "FAIL", scope: "PRODUCT_LIFECYCLE", launches: []};
let browser, proxy, active;
const stage = async name => {
  report.stage = name; console.log(name);
  await writeFile(join(directory, "dosbox-product.json"), JSON.stringify(report, null, 2) + "\n");
};
try {
  assert.ok(base && env.RETROM_CHROME_EXECUTABLE && env.RETROM_DOS_GAME && env.RETROM_DOS_ENTRY, "DOS_OPERATOR_INPUT_REQUIRED");
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(15000); await installVirtualStandardGamepad(context); await observeDOSStates(context);
  const collector = await observeContentStoreEvents(context, {retain: true}), client = await fantasyClient(context, base);
  report.dosEntry = env.RETROM_DOS_ENTRY;
  report.sources = [await computerSource(env.RETROM_DOS_GAME)];
  const review = await importComputer(client, "dos", "dosbox_pure", env.RETROM_DOS_GAME);
  report.import = {itemId: review.itemId, importJobId: review.importJobId};
  const response = await client.raw("GET", `/api/v1/admin/reviews/${review.itemId}`), details = await response.json();
  assert.ok(details.sourceFiles.some(row => row.sha256 === report.sources[0].sha256 && row.sizeBytes === report.sources[0].sizeBytes));
  assert.ok(details.dosEntries.some(row => row.path === env.RETROM_DOS_ENTRY && row.enabled && row.directLaunchSafe));
  await client.json("PATCH", `/api/v1/admin/reviews/${review.itemId}`, {
    headers: {...client.writeHeaders(), "If-Match": response.headers().etag}, data: {defaultDosEntry: env.RETROM_DOS_ENTRY, tagIds: []},
  });
  await stage("import");
  const preview = active = await openDOS(context, base, await previewCart(client, review.itemId));
  report.content = {contentDigest: preview.contentDigest, sizeBytes: preview.source.sizeBytes};
  report.preview = {boot: await bootDoom(preview), input: await moveAndFireDoom(preview), screenshot: await pictureComputer(preview, directory, "preview")};
  await preview.flush(); report.cold = preview.rangeSummary(); assert.ok(report.cold.requests > 0);
  report.launches.push(await closeComputer(preview, base, collector)); await stage("preview");
  report.gameId = (await approveCart(client, review.itemId)).gameId;
  const launch = await launchCart(client, report.gameId, null, report.dosEntry); launch.returnTo = `/games/${report.gameId}`;
  const opened = active = await openDOS(context, base, launch);
  assert.equal(opened.contentDigest, report.content.contentDigest);
  report.boot = await bootDoom(opened); report.input = await moveAndFireDoom(opened);
  await pauseComputer(opened); report.savedScene = await doomScene(opened);
  report.savedScreenshot = await pictureComputer(opened, directory, "saved");
  report.save = await saveCart(opened.page, launch.launchId, "dosbox_pure");
  report.nativeSave = await opened.frame.evaluate(async () => {
    await Promise.all(globalThis.__dosStateObservation.pending); return globalThis.__dosStateObservation.captures.at(-1);
  });
  assert.ok(report.nativeSave?.sizeBytes > 0, "DOS_NATIVE_CAPTURE_MISSING");
  report.launches.push(await closeComputer(opened, base, collector)); await stage("save");
  const next = await launchCart(client, report.gameId, report.save.saveStateId); next.returnTo = launch.returnTo;
  assert.notEqual(next.launchId, launch.launchId);
  const restored = active = await openDOS(context, base, next);
  assert.equal(restored.contentDigest, report.content.contentDigest);
  report.nativeRestore = await restored.frame.evaluate(() => globalThis.__dosStateObservation.restores.at(-1));
  assert.deepEqual(report.nativeRestore, report.nativeSave, "DOS_NATIVE_RESTORE_MISMATCH");
  await restored.page.waitForTimeout(300); await pauseComputer(restored);
  report.restoredScene = await doomScene(restored); report.restoredScreenshot = await pictureComputer(restored, directory, "restored");
  assert.deepEqual(report.restoredScene.ammo, report.savedScene.ammo, "DOS_RESTORED_AMMUNITION_MISMATCH");
  assert.deepEqual(report.restoredScene.wall, report.savedScene.wall, "DOS_RESTORED_VIEW_MISMATCH");
  await restored.page.getByRole("button", {name: "继续游戏", exact: true}).click();
  await restored.canvas.click();
  report.restoredInput = await moveAndFireDoom(restored, 14);
  await restored.network.flush(); assert.equal(restored.network.requests.length, 0, "DOS_WARM_BODY_REQUESTS");
  report.launches.push(await closeComputer(restored, base, collector)); report.status = "PASS";
} catch (error) {
  report.errorCode = error.message; report.stack = error.stack; process.exitCode = 1;
  await active?.page.screenshot({path: join(directory, "failure.png")}).catch(() => {});
} finally {
  await browser?.close(); await proxy?.close();
  await writeFile(join(directory, "dosbox-product.json"), JSON.stringify(report, null, 2) + "\n");
  console.log(JSON.stringify({caseId: report.caseId, status: report.status, errorCode: report.errorCode}));
}

if (report.status === "PASS" && env.RETROM_CONTENT_IO_FULL_PROOF === "1") await completeDOSContentProof(directory, report);
