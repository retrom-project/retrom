import assert from "node:assert/strict";
import {mkdir, writeFile} from "node:fs/promises";
import {join, resolve} from "node:path";
import {randomUUID} from "node:crypto";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, approveCart, launchCart} from "./fantasy_product_client.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import {performContentIOPlayerExit} from "./content_io_player_exit.mjs";
import {computerSource, installComputerBios, importComputer} from "./computer_product_client.mjs";
import {openComputer, pictureComputer, closeComputer, collectComputerExit, saveComputerDisk} from "./computer_product_browser.mjs";
import {bootSafari, moveSafari, samDisk, samCommand, samProgramOutput, writeSamProgram} from "./samcoupe_product_browser.mjs";
import {completeComputerContentProof} from "./computer_content_proof.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/samcoupe-product"); await mkdir(directory, {recursive: true});
const report = {schemaVersion: 1, caseId: "ACC-SAMCOUPE-001", runId: env.RETROM_CONTENT_IO_RUN_ID ?? randomUUID(),
  status: "FAIL", scope: "PRODUCT_LIFECYCLE", launches: []};
let browser, proxy, active;
const stage = async name => {
  report.stage = name; console.log(name);
  await writeFile(join(directory, "samcoupe-product.json"), JSON.stringify(report, null, 2) + "\n");
};
try {
  assert.ok(base && env.RETROM_CHROME_EXECUTABLE && env.RETROM_SAM_GAME && env.RETROM_SAM_WRITABLE_DISK && env.RETROM_SAM_BIOS,
    "SAM_OPERATOR_INPUT_REQUIRED");
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.setDefaultTimeout(15000); await installVirtualStandardGamepad(context);
  const collector = await observeContentStoreEvents(context, {retain: true}), client = await fantasyClient(context, base);
  const bios = await installComputerBios(client, "samcoupeweb", {"samcoupe.rom": env.RETROM_SAM_BIOS});
  const gameSources = [await computerSource(env.RETROM_SAM_GAME), ...bios];
  const diskSources = [await computerSource(env.RETROM_SAM_WRITABLE_DISK), ...bios];
  report.sources = [gameSources[0], diskSources[0], ...bios];
  const open = async (launch, sources) => {
    active = await openComputer(context, base, launch, "samcoupe", sources); return active;
  };
  const gameReview = await importComputer(client, "samcoupe", "samcoupeweb", env.RETROM_SAM_GAME);
  report.gameImport = {itemId: gameReview.itemId, importJobId: gameReview.importJobId}; await stage("game-import");
  const preview = await open(await previewCart(client, gameReview.itemId), gameSources);
  report.preview = {player: await bootSafari(preview), input: await moveSafari(preview), screenshot: await pictureComputer(preview, directory, "preview")};
  report.launches.push(await closeComputer(preview, base, collector, "GAME_SAVE"));
  report.gameId = (await approveCart(client, gameReview.itemId)).gameId; await stage("game-published");
  const gameLaunch = await launchCart(client, report.gameId); gameLaunch.returnTo = `/games/${report.gameId}`;
  const game = await open(gameLaunch, gameSources);
  report.game = {player: await bootSafari(game), input: await moveSafari(game), screenshot: await pictureComputer(game, directory, "game-input")};
  await game.network.flush(); assert.equal(game.network.requests.length, 0, "SAM_WARM_GAME_NETWORK");
  report.launches.push(await closeComputer(game, base, collector, "GAME_SAVE")); await stage("game-input");

  const diskReview = await importComputer(client, "samcoupe", "samcoupeweb", env.RETROM_SAM_WRITABLE_DISK);
  report.diskImport = {itemId: diskReview.itemId, importJobId: diskReview.importJobId};
  const trial = await open(await previewCart(client, diskReview.itemId), diskSources);
  await trial.page.waitForTimeout(4000); report.diskPreview = await pictureComputer(trial, directory, "native-disk-preview");
  report.launches.push(await closeComputer(trial, base, collector, "GAME_SAVE"));
  report.diskGameId = (await approveCart(client, diskReview.itemId)).gameId;
  const launch = await launchCart(client, report.diskGameId); launch.returnTo = `/games/${report.diskGameId}`;
  const opened = await open(launch, diskSources); await opened.page.waitForTimeout(4000);
  report.disk = await writeSamProgram(opened);
  report.output = await samProgramOutput(opened); report.savedScreenshot = await pictureComputer(opened, directory, "native-save");
  // SAM has no instant checkpoint button: the public GAME_SAVE exit flow exports the native disk.
  report.save = await performContentIOPlayerExit(opened.page, base, launch, () => saveComputerDisk(opened.page, launch.launchId));
  report.launches.push(await collectComputerExit(opened, collector)); await stage("native-save");
  const next = await launchCart(client, report.diskGameId, report.save.saveStateId); next.returnTo = launch.returnTo;
  assert.notEqual(next.launchId, launch.launchId);
  const restored = await open(next, diskSources); await restored.page.waitForTimeout(4000);
  report.restoredDisk = await samDisk(restored); assert.deepEqual(report.restoredDisk, report.disk.after, "SAM_NATIVE_DISK_RESTORE_MISMATCH");
  await samCommand(restored, "LOAD CHR$ 82"); await restored.page.waitForTimeout(1500);
  report.restoredOutput = await samProgramOutput(restored);
  assert.deepEqual(report.restoredOutput, report.output, "SAM_NATIVE_LOAD_OUTPUT_MISMATCH");
  report.restoredScreenshot = await pictureComputer(restored, directory, "native-restored");
  await restored.network.flush(); assert.equal(restored.network.requests.length, 0, "SAM_WARM_DISK_NETWORK");
  report.launches.push(await closeComputer(restored, base, collector, "GAME_SAVE")); report.status = "PASS";
} catch (error) {
  report.errorCode = error.message; report.stack = error.stack; process.exitCode = 1;
  await active?.page.screenshot({path: join(directory, "failure.png")}).catch(() => {});
} finally {
  await browser?.close(); await proxy?.close();
  await writeFile(join(directory, "samcoupe-product.json"), JSON.stringify(report, null, 2) + "\n");
  console.log(JSON.stringify({caseId: report.caseId, status: report.status, errorCode: report.errorCode}));
}
if (report.status === "PASS" && env.RETROM_CONTENT_IO_FULL_PROOF === "1") await completeComputerContentProof(directory, report, "samcoupe");
