import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {existsSync, mkdirSync, readFileSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import sharp from "../../web/node_modules/sharp/dist/index.mjs";

import {fantasyClient, previewCart, approveCart, launchCart, gamepad} from "./fantasy_product_client.mjs";
import {singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {waitForPreviewReady, revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";

const env = process.env;
const cases = {
  naomi: {core: "flycast-naomi", bios: "naomi.zip"},
  naomi2: {core: "flycast-naomi2", bios: "naomi2.zip"},
  atomiswave: {core: "flycast-atomiswave", bios: "awbios.zip"},
};
const platform = env.RETROM_FLYCAST_ARCADE_PLATFORM;
const scenario = cases[platform];
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? `.artifacts/flycast-${platform}`);
const evidence = {caseId: `ACC-FLYCAST-ARCADE-${platform}`, status: "FAIL", stages: [], errors: [], consoleErrors: [],
  invalidWebglCapabilities: 0, rangeResponses: 0};
let browser, proxy;
const requests = [], gameUrls = new Set();

async function openPlayer(context, playUrl, launchId, expectedTarget) {
  const page = await context.newPage();
  page.on("pageerror", error => evidence.errors.push((error.stack ?? error.message).slice(0, 1200)));
  await page.goto(env.RETROM_ACCEPTANCE_BASE_URL + playUrl, {waitUntil: "domcontentloaded"});
  await waitForPreviewReady(page);
  let frame;
  for (const candidate of page.frames().filter(candidate => candidate !== page.mainFrame())) {
    if (await candidate.evaluate(() => Boolean(window.EJS_emulator?.gameManager))) {frame = candidate; break;}
  }
  assert.ok(frame, "FLYCAST_ARCADE_FRAME_MISSING");
  assert.ok(await frame.evaluate(() => Boolean(window.EJS_emulator?.gameManager)), "FLYCAST_ARCADE_INSTANCE_MISSING");
  const gameName = await frame.evaluate(() => window.EJS_gameUrl?.name);
  assert.equal(gameName, singleFile(env.RETROM_FLYCAST_ARCADE_ROM)[0].relativePath);
  assert.equal(await frame.evaluate(() => window.EJS_emulator?.fileName), gameName);
  const config = await page.evaluate(async id => (await fetch(`/runtime/launches/${id}/config`)).json(), launchId);
  assert.equal(config.runtime.targetId, expectedTarget);
  const game = config.resources.find(resource => resource.role === "game");
  assert.equal(game?.kind, "SEEKABLE_BLOB");
  assert.equal(game.rangeRequired, true);
  gameUrls.add(new URL(game.url, env.RETROM_ACCEPTANCE_BASE_URL).href);
  const bios = config.resources.find(resource => resource.role === "external")?.files ?? [];
  assert.ok(bios.some(file => file.virtualPath === `dc/${scenario.bios}`), "FLYCAST_ARCADE_BIOS_NOT_MOUNTED");
  const mountedBiosBytes = await frame.evaluate(name => window.EJS_emulator.gameManager.FS.stat(`/dc/${name}`).size,
    scenario.bios);
  assert.ok(mountedBiosBytes > 0, "FLYCAST_ARCADE_BIOS_BYTES_MISSING");
  const canvas = frame.locator("canvas").first();
  await canvas.click();
  return {page, frame, canvas, config};
}

async function assertVisibleFrame(page, canvas, path) {
  const deadline = Date.now() + Number(env.RETROM_FLYCAST_ARCADE_VISIBLE_TIMEOUT_MS ?? 180_000);
  do {
    const png = await canvas.screenshot({path});
    const metadata = await sharp(png).metadata();
    const width = Math.floor(metadata.width * 0.8), height = Math.floor(metadata.height * 0.8);
    const {data} = await sharp(png).extract({left: Math.floor(metadata.width * 0.1),
      top: Math.floor(metadata.height * 0.1), width, height}).removeAlpha().raw().toBuffer({resolveWithObject: true});
    let bright = 0, dark = 0;
    for (let i = 0; i < data.length; i += 3) {
      if (Math.max(data[i], data[i + 1], data[i + 2]) > 80) {bright++;}
      if (Math.min(data[i], data[i + 1], data[i + 2]) < 40) {dark++;}
    }
    evidence.frameFractions = {bright: bright / (width * height), dark: dark / (width * height)};
    if (evidence.frameFractions.bright > 0.01 && evidence.frameFractions.dark > 0.01) {return;}
    await page.waitForTimeout(3000);
  } while (Date.now() < deadline);
  assert.fail("FLYCAST_ARCADE_BLACK_FRAME");
}

async function assertGameFrame(canvas, path) {
  const png = await canvas.screenshot({path});
  const stats = await sharp(png).greyscale().stats();
  const {mean, stdev} = stats.channels[0];
  assert.ok(mean > 20 && stdev > 20, `FLYCAST_ARCADE_GAME_FRAME_MISSING:${mean}:${stdev}`);
}

try {
  assert.ok(scenario && [env.RETROM_ACCEPTANCE_BASE_URL, env.RETROM_ACCEPTANCE_USERNAME,
    env.RETROM_ACCEPTANCE_PASSWORD, env.RETROM_CHROME_EXECUTABLE, env.RETROM_FLYCAST_ARCADE_ROM,
    env.RETROM_FLYCAST_ARCADE_BIOS_DIR].every(Boolean), "FLYCAST_ARCADE_INPUT_REQUIRED");
  mkdirSync(directory, {recursive: true});
  evidence.platform = platform;
  evidence.gameSha256 = createHash("sha256").update(readFileSync(env.RETROM_FLYCAST_ARCADE_ROM)).digest("hex");
  const progressPath = join(directory, "progress.json");
  const progress = existsSync(progressPath) ? JSON.parse(readFileSync(progressPath, "utf8")) : {};
  if (progress.gameSha256) {assert.equal(progress.gameSha256, evidence.gameSha256);}
  proxy = await localRpgAcceptanceProxy(env.RETROM_ACCEPTANCE_BASE_URL);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--autoplay-policy=no-user-gesture-required"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  context.on("response", response => requests.push({url: response.url(), status: response.status(),
    method: response.request().method(), range: response.request().headers().range}));
  context.on("console", message => {
    const line = message.text();
    if (/WebGL: INVALID_ENUM: (?:en|dis)able: invalid capability/iu.test(line)) {evidence.invalidWebglCapabilities++;}
    if (message.type() === "error" && evidence.consoleErrors.length < 20) {evidence.consoleErrors.push(line.slice(0, 500));}
  });
  context.setDefaultTimeout(30_000);
  await installVirtualStandardGamepad(context);
  const client = await fantasyClient(context, env.RETROM_ACCEPTANCE_BASE_URL);
  const requirements = (await client.json("GET", `/api/v1/admin/bios?scope=FULL_CATALOG&coreId=${scenario.core}&limit=100`)).items;
  const firmware = requirements.find(item => item.coreId === scenario.core && item.logicalName === scenario.bios);
  assert.ok(firmware, "FLYCAST_ARCADE_BIOS_REQUIREMENT_MISSING");
  if (!["MATCHED", "HASH_WARNING"].includes(firmware.status)) {
    const uploadId = await client.upload(singleFile(join(env.RETROM_FLYCAST_ARCADE_BIOS_DIR, scenario.bios)), "FILES", "GENERAL");
    const upload = await client.json("GET", `/api/v1/admin/uploads/${uploadId}`);
    await client.json("POST", `/api/v1/admin/bios/${firmware.id}/installations`, {expected: 201,
      headers: {...client.writeHeaders(), "If-Match": `"v${firmware.version}"`},
      data: {uploadFileId: upload.files[0].fileId}});
  }
  evidence.stages.push("bios-installed");

  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200});
  const instances = await client.json("GET", `/api/v1/admin/platform-instances?platformId=${platform}&limit=100`);
  const instance = instances.items.find(item => item.enabled && item.defaultCoreId === scenario.core);
  assert.ok(instance, "FLYCAST_ARCADE_PLATFORM_MISSING");
  if (!progress.reviewId && !progress.gameId) {
    const uploadId = await client.upload(singleFile(env.RETROM_FLYCAST_ARCADE_ROM), "FILES", "GENERAL");
    const imported = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
      data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []}});
    const review = await reviewForImport(client, imported.importJobId);
    progress.reviewId = review.itemId;
    progress.gameSha256 = evidence.gameSha256;
    writeFileSync(progressPath, JSON.stringify(progress));
  }
  if (!progress.gameId) {
    const review = await client.json("GET", `/api/v1/admin/reviews/${progress.reviewId}`);
    assert.equal(review.validation.status, "READY");
    evidence.stages.push("import-review-ready");
    const preview = await previewCart(client, review.itemId);
    const first = await openPlayer(context, preview.playUrl, preview.previewId, scenario.core);
    await assertVisibleFrame(first.page, first.canvas, join(directory, "preview.png"));
    await gamepad(first.page, 13, 250);
    await first.page.waitForTimeout(1000);
    await first.canvas.screenshot({path: join(directory, "preview-direction.png")});
    await first.page.close();
    evidence.stages.push("preview-input");
    progress.gameId = (await approveCart(client, review.itemId)).gameId;
    writeFileSync(progressPath, JSON.stringify(progress));
  }
  evidence.gameId = progress.gameId;
  const launch = await launchCart(client, progress.gameId);
  const second = await openPlayer(context, launch.playUrl, launch.launchId, scenario.core);
  await assertVisibleFrame(second.page, second.canvas, join(directory, "product.png"));
  if (env.RETROM_FLYCAST_ARCADE_GAMEPLAY_WAIT_MS) {
    await second.page.waitForTimeout(Number(env.RETROM_FLYCAST_ARCADE_GAMEPLAY_WAIT_MS));
    await assertGameFrame(second.canvas, join(directory, "gameplay.png"));
    evidence.stages.push("game-frame");
  }
  await revealPreviewToolbar(second.page);
  const savedResponse = second.page.waitForResponse(response => response.request().method() === "POST" &&
    new URL(response.url()).pathname === `/runtime/launches/${launch.launchId}/save-states`, {timeout: 60_000});
  await second.page.getByRole("button", {name: "创建存档", exact: true}).click();
  assert.equal((await savedResponse).status(), 201);
  const saves = await client.json("GET", `/api/v1/saves?gameId=${progress.gameId}&limit=100`);
  const saved = saves.items[0];
  assert.ok(saved?.saveStateId, "FLYCAST_ARCADE_SAVE_MISSING");
  evidence.saveStateId = saved.saveStateId;
  await second.page.close();
  evidence.stages.push("product-checkpoint");

  const restored = await launchCart(client, progress.gameId, saved.saveStateId);
  const third = await openPlayer(context, restored.playUrl, restored.launchId, scenario.core);
  assert.equal(third.config.restore?.format, "flycast-state-v1-storage-v1");
  await assertVisibleFrame(third.page, third.canvas, join(directory, "restored.png"));
  if (env.RETROM_FLYCAST_ARCADE_GAMEPLAY_WAIT_MS) {
    await assertGameFrame(third.canvas, join(directory, "restored-gameplay.png"));
  }
  await gamepad(third.page, 0, 250);
  if (env.RETROM_FLYCAST_ARCADE_GAMEPLAY_WAIT_MS) {
    await third.page.waitForTimeout(1000);
    await assertGameFrame(third.canvas, join(directory, "restored-input.png"));
  }
  await third.page.close();
  evidence.stages.push("restore-input");
  const gameRequests = requests.filter(request => gameUrls.has(request.url) && request.method === "GET");
  evidence.rangeResponses = gameRequests.filter(request => request.status === 206 && request.range).length;
  assert.ok(evidence.rangeResponses > 0, "FLYCAST_ARCADE_RANGE_NOT_USED");
  assert.ok(gameRequests.every(request => request.status === 206 && request.range), "FLYCAST_ARCADE_FULL_ROM_DOWNLOAD");
  assert.equal(evidence.invalidWebglCapabilities, 0, "FLYCAST_ARCADE_INVALID_WEBGL_CAPABILITY");
  assert.deepEqual(evidence.errors, []);
  evidence.status = "AWAITING_VISUAL_REVIEW";
} catch (error) {
  evidence.error = error.stack?.slice(0, 1000) ?? String(error);
  process.exitCode = 1;
} finally {
  evidence.observedRanges = requests.filter(request => request.status === 206 && request.range).length;
  await browser?.close();
  await proxy?.close();
  if (scenario) {
    mkdirSync(directory, {recursive: true});
    writeFileSync(join(directory, "evidence.json"), JSON.stringify(evidence, null, 2));
  }
  console.log(JSON.stringify(evidence));
}
