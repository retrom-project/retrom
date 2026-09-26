import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, previewCart, approveCart, launchCart} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";
import {observeExpansion, openExpansion, pictureExpansion, pressExpansion, holdExpansion, saveExpansion, waitExpansionFrames} from "./platform_expansion_browser.mjs";

const env = process.env, platform = process.argv[2], base = env.RETROM_ACCEPTANCE_BASE_URL;
const cores = {gamegear: "genesis_plus_gx", sg1000: "genesis_plus_gx", multivision: "genesis_plus_gx",
  pico: "picodrive", sega32x: "picodrive", supergrafx: "mednafen_pce", gx4000: "cap32", neogeo: "fbneo",
  sgb: "gearboy", segacd: "genesis_plus_gx", amiga: "puae", amigacd32: "puae", satellaview: "snes9x"};
const biosFiles = {segacd: ["bios_CD_E.bin", "bios_CD_U.bin", "bios_CD_J.bin"],
  amiga: ["kick34005.A500", "kick40068.A1200"],
  amigacd32: ["kick40060.CD32", "kick40060.CD32.ext"], satellaview: ["BS-X.bin"]};
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? `.artifacts/platform-expansion/${platform}`);
const evidence = {caseId: biosFiles[platform] ? "ACC-RUN-018" : "ACC-RUN-017",
  platform, status: "FAIL", stages: [], errors: [], runtimes: []};
mkdirSync(directory, {recursive: true});
let browser, proxy;
const coreReads = [];
const stage = name => {evidence.stages.push(name); console.log(`${platform}: ${name}`); flush();};
function flush() {writeFileSync(join(directory, "product.json"), JSON.stringify(evidence, null, 2) + "\n");}

async function installBIOS(client) {
  const names = platform === "neogeo" ? ["neogeo.zip"] : biosFiles[platform] ?? [];
  if (!names.length) {return;}
  const sourceDir = env.RETROM_EXPANSION_BIOS_DIR;
  assert.ok(platform === "neogeo" ? env.RETROM_EXPANSION_BIOS : sourceDir, "PLATFORM_BIOS_SOURCE_REQUIRED");
  const catalog = await client.json("GET", `/api/v1/admin/bios?scope=FULL_CATALOG&coreId=${cores[platform]}&limit=100`);
  for (const name of names) {
    const item = catalog.items.find(entry => entry.logicalName === name && entry.enabled &&
      (platform !== "segacd" || entry.targetId === "genesis-plus-gx-cd"));
    assert.ok(item, `BIOS_REQUIREMENT_MISSING:${name}`);
    if (item.activeInstallation) {continue;}
    const path = platform === "neogeo" ? env.RETROM_EXPANSION_BIOS : join(sourceDir, name);
    const uploadId = await client.upload(singleFile(path), "FILES", "GENERAL");
    const upload = await client.json("GET", `/api/v1/admin/uploads/${uploadId}`);
    await client.json("POST", `/api/v1/admin/bios/${item.id}/installations`, {
      headers: {...client.writeHeaders(), "If-Match": `"v${item.version}"`}, expected: 201,
      data: {uploadFileId: upload.files[0].fileId},
    });
  }
}

async function importGame(client) {
  if (env.RETROM_EXPANSION_REVIEW_ID) {
    evidence.reusedReview = true; return env.RETROM_EXPANSION_REVIEW_ID;
  }
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  const id = platform === "neogeo" ? "arcade" : platform;
  const directories = await client.json("GET", `/api/v1/admin/platform-instances?platformId=${id}&limit=100`);
  const instance = directories.items.find(item => item.enabled && item.defaultCoreId === cores[platform]);
  assert.ok(instance, "PLATFORM_DIRECTORY_MISSING");
  const uploadId = await client.upload(singleFile(env.RETROM_EXPANSION_ROM), "FILES", "GENERAL");
  const imported = await client.json("POST", "/api/v1/admin/imports", {
    headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []},
  });
  return (await reviewForImport(client, imported.importJobId)).itemId;
}

async function exercise(opened) {
  evidence.bootFrames = await waitExpansionFrames(opened, Number(env.RETROM_EXPANSION_BOOT_FRAMES ?? 600));
  await pictureExpansion(opened, directory, "ready-for-start");
  const defaults = ["sg1000", "multivision"].includes(platform) ? "[1]" : platform === "neogeo" ? "[8,9,0]" : "[9,0]";
  const buttons = JSON.parse(env.RETROM_EXPANSION_START_BUTTONS ?? defaults);
  evidence.recipe = {buttons, bootFrames: Number(env.RETROM_EXPANSION_BOOT_FRAMES ?? 600),
    gapFrames: Number(env.RETROM_EXPANSION_START_GAP_FRAMES ?? 0),
    postStartFrames: Number(env.RETROM_EXPANSION_POST_START_FRAMES ?? 180),
    postConfirmFrames: Number(env.RETROM_EXPANSION_POST_CONFIRM_FRAMES ?? 0),
    direction: Number(env.RETROM_EXPANSION_DIRECTION ?? 15),
    holdButton: env.RETROM_EXPANSION_HOLD_BUTTON ?? null,
    confirmButton: Number(env.RETROM_EXPANSION_CONFIRM_BUTTON ?? (["sg1000", "multivision"].includes(platform) ? 1 : 0))};
  flush();
  for (const [index, button] of buttons.entries()) {
    if (index && Number(env.RETROM_EXPANSION_START_GAP_FRAMES ?? 0)) {
      await waitExpansionFrames(opened, Number(env.RETROM_EXPANSION_START_GAP_FRAMES));
    }
    await pressExpansion(opened, button, 400);
    await pictureExpansion(opened, directory, `start-${index + 1}`);
  }
  evidence.startFrames = await waitExpansionFrames(opened, Number(env.RETROM_EXPANSION_POST_START_FRAMES ?? 180));
  await holdExpansion(opened, env.RETROM_EXPANSION_HOLD_BUTTON, true);
  if (env.RETROM_EXPANSION_HOLD_BUTTON !== undefined) {await waitExpansionFrames(opened, 120);}
  const before = await pictureExpansion(opened, directory, "before-input");
  const direction = Number(env.RETROM_EXPANSION_DIRECTION ?? 15);
  assert.ok([12, 13, 14, 15].includes(direction), "DIRECTION_INVALID");
  await pressExpansion(opened, direction, 600);
  const moved = await pictureExpansion(opened, directory, "direction");
  const confirmButton = Number(env.RETROM_EXPANSION_CONFIRM_BUTTON ?? (["sg1000", "multivision"].includes(platform) ? 1 : 0));
  await pressExpansion(opened, confirmButton, 250);
  if (Number(env.RETROM_EXPANSION_POST_CONFIRM_FRAMES ?? 0) > 0) {
    evidence.confirmFrames = await waitExpansionFrames(opened, Number(env.RETROM_EXPANSION_POST_CONFIRM_FRAMES));
  }
  const confirmed = await pictureExpansion(opened, directory, "confirm");
  await holdExpansion(opened, env.RETROM_EXPANSION_HOLD_BUTTON, false);
  const inputs = await opened.frame.evaluate(() => window.__expansion.inputs);
  evidence.input = {before, moved, confirmed, events: inputs};
  assert.ok(inputs.some(([player, control, value]) => player === 0 && control === direction - 8 && value === 1),
    "DIRECTION_NOT_DELIVERED");
  assert.ok(inputs.some(([, control, value]) => [0, 1, 8].includes(control) && value === 1), "CONFIRM_NOT_DELIVERED");
  assert.notEqual(before, moved, "DIRECTION_SCREEN_UNCHANGED");
}

try {
  assert.ok(cores[platform] && base && env.RETROM_EXPANSION_ROM && env.RETROM_CHROME_EXECUTABLE, "PLATFORM_INPUT_REQUIRED");
  const bytes = readFileSync(env.RETROM_EXPANSION_ROM);
  evidence.source = {sha256: createHash("sha256").update(bytes).digest("hex"), sizeBytes: bytes.length};
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: env.RETROM_SMOKE_HEADED !== "1",
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  evidence.chromeVersion = browser.version();
  evidence.browserMode = env.RETROM_SMOKE_HEADED === "1" ? "headed-xvfb" : "headless";
  const viewport = {width: Number(env.RETROM_EXPANSION_VIEWPORT_WIDTH ?? 1280),
    height: Number(env.RETROM_EXPANSION_VIEWPORT_HEIGHT ?? 900)};
  assert.ok(Number.isInteger(viewport.width) && viewport.width > 0 &&
    Number.isInteger(viewport.height) && viewport.height > 0, "PLATFORM_VIEWPORT_INVALID");
  evidence.viewport = viewport;
  const context = await browser.newContext({viewport, ...proxy.contextOptions});
  evidence.coreResponses = [];
  context.on("response", response => {
    const coreAsset = ["amiga", "amigacd32"].includes(platform) ? "puae-thread-wasm.data" : `${cores[platform]}-wasm.data`;
    if (!new URL(response.url()).pathname.endsWith(`/${coreAsset}`)) {return;}
    coreReads.push((async () => {
      const bytes = await response.body();
      assert.equal(response.status(), 200, "CORE_DOWNLOAD_FAILED");
      evidence.coreResponses.push({path: new URL(response.url()).pathname, sizeBytes: bytes.length,
        sha256: createHash("sha256").update(bytes).digest("hex")});
    })().catch(error => evidence.errors.push(error.message.split("\n")[0])));
  });
  context.setDefaultTimeout(30000);
  await installVirtualStandardGamepad(context); await observeExpansion(context);
  const client = await fantasyClient(context, base);
  await installBIOS(client);
  let gameId = env.RETROM_EXPANSION_GAME_ID;
  if (!gameId) {
    const itemId = await importGame(client); evidence.itemId = itemId; stage("import");
    const preview = await previewCart(client, itemId), trial = await openExpansion(context, base, preview, evidence);
    await waitExpansionFrames(trial, 600); await pictureExpansion(trial, directory, "review-preview");
    await revealPreviewToolbar(trial.page);
    const screenshot = trial.page.waitForResponse(response => response.request().method() === "POST" &&
      new URL(response.url()).pathname.endsWith("/review-screenshot"));
    await trial.page.getByRole("button", {name: "保存审核截图", exact: true}).click();
    assert.ok((await screenshot).ok(), "REVIEW_SCREENSHOT_FAILED");
    await trial.page.close(); stage("review-preview");
    gameId = (await approveCart(client, itemId)).gameId; stage("publish");
  } else {evidence.reusedGame = true;}
  evidence.gameId = gameId; flush();
  const launch = await launchCart(client, gameId), opened = await openExpansion(context, base, launch, evidence);
  await exercise(opened); stage("product-input");
  const saved = await saveExpansion(opened, client, launch, gameId);
  assert.ok(saved.sizeBytes > 0); evidence.save = {saveStateId: saved.saveStateId, sizeBytes: saved.sizeBytes};
  const saveImage = await client.raw("GET", `/content/save-states/${saved.saveStateId}/screenshot`);
  assert.equal(saveImage.status(), 200, "SAVE_SCREENSHOT_MISSING");
  writeFileSync(join(directory, "saved.png"), await saveImage.body());
  await opened.page.close(); stage("save");
  const restored = await launchCart(client, gameId, saved.saveStateId);
  assert.notEqual(restored.launchId, launch.launchId);
  const resumed = await openExpansion(context, base, restored, evidence);
  evidence.restoreReceipts = await resumed.frame.evaluate(() => window.__expansion.restores);
  assert.equal(evidence.restoreReceipts.length, 1, "NATIVE_RESTORE_RECEIPT_MISSING");
  const restoreBefore = await pictureExpansion(resumed, directory, "restored");
  await holdExpansion(resumed, env.RETROM_EXPANSION_HOLD_BUTTON, true);
  if (env.RETROM_EXPANSION_HOLD_BUTTON !== undefined) {await waitExpansionFrames(resumed, 120);}
  const restoreButton = Number(env.RETROM_EXPANSION_RESTORE_BUTTON ?? env.RETROM_EXPANSION_RESTORE_DIRECTION ?? 14);
  assert.ok([9, 12, 13, 14, 15].includes(restoreButton), "RESTORE_BUTTON_INVALID");
  await pressExpansion(resumed, restoreButton, 600);
  if (platform === "satellaview") {await pressExpansion(resumed, 0, 250);}
  const restoreAfter = await pictureExpansion(resumed, directory, "restored-input");
  evidence.restoredInput = {before: restoreBefore, after: restoreAfter};
  assert.notEqual(restoreBefore, restoreAfter, "RESTORED_INPUT_SCREEN_UNCHANGED");
  await holdExpansion(resumed, env.RETROM_EXPANSION_HOLD_BUTTON, false);
  const inputs = await resumed.frame.evaluate(() => window.__expansion.inputs);
  assert.ok(inputs.some(([, control, value]) => control === (restoreButton === 9 ? 3 : restoreButton - 8) && value === 1),
    "RESTORED_INPUT_NOT_DELIVERED");
  await resumed.page.close(); stage("different-launch-restore-input");
  if (platform === "segacd") {
    const reads = evidence.contentResponses.filter(response => response.path.includes("/content/game/"));
    assert.ok(reads.length > 0, "SEEKABLE_RANGE_EVIDENCE_MISSING");
    assert.ok(reads.every(response => response.status === 206 && /^bytes=\d+-\d+$/u.test(response.range ?? "") &&
      response.contentLength > 0 && response.contentLength <= 524288), "SEEKABLE_RANGE_INVALID");
    const cachedRanges = new Map();
    for (const response of reads) {
      const key = `${response.path}|${response.range}`;
      const firstLaunch = cachedRanges.get(key);
      assert.ok(!firstLaunch || firstLaunch === response.launchId, "SEEKABLE_CACHED_RANGE_REDOWNLOADED");
      cachedRanges.set(key, response.launchId);
    }
    stage("seekable-cache-reuse");
  }
  if (["amiga", "amigacd32"].includes(platform)) {
    const reads = evidence.contentResponses.filter(response => response.path.includes("/content/game/"));
    assert.ok(reads.length > 0, "EAGER_CONTENT_DOWNLOAD_EVIDENCE_MISSING");
    assert.ok(reads.every(response => response.status === 200 && response.range === null), "EAGER_CONTENT_TRANSPORT_INVALID");
    assert.ok(reads.filter(response => response.launchId === launch.launchId).length <= 1, "EAGER_CONTENT_DUPLICATE_DOWNLOAD");
    assert.ok(!reads.some(response => response.launchId === restored.launchId), "EAGER_CONTENT_REDOWNLOADED");
    stage("eager-cache-reuse");
  }
  await Promise.all(coreReads);
  if (env.RETROM_EXPANSION_CORE_SHA256) {
    assert.ok(evidence.coreResponses.length > 0, "CORE_DOWNLOAD_EVIDENCE_MISSING");
    assert.ok(evidence.coreResponses.every(file => file.sha256 === env.RETROM_EXPANSION_CORE_SHA256), "CORE_DOWNLOAD_DIGEST_MISMATCH");
  }
  assert.deepEqual(evidence.errors, []);
  evidence.status = "AUTOMATED_PASS_REQUIRES_VISUAL_REVIEW";
} catch (error) {
  evidence.error = error.message.split("\n")[0].slice(0, 250); process.exitCode = 1;
  flush(); console.log(`${platform}: ${evidence.error}`);
  const page = browser?.contexts()[0]?.pages().at(-1);
  if (page) {await page.screenshot({path: join(directory, "failure.png"), timeout: 5000}).catch(() => undefined);}
} finally {
  await browser?.close(); await proxy?.close(); flush();
  console.log(JSON.stringify({platform, status: evidence.status, error: evidence.error}));
}
