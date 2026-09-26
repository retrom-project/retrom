import assert from "node:assert/strict";
import {appendFileSync, mkdirSync, readFileSync, writeFileSync, existsSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import sharp from "../../web/node_modules/sharp/dist/index.cjs";
import {px68kLocalProxy} from "./px68k_product_support.mjs";
import {fantasyClient, previewCart, approveCart, launchCart, gamepad} from "./fantasy_product_client.mjs";
import {directoryFiles, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {captureOptionalReviewScreenshot, revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";

const env = process.env;
const base = env.RETROM_ACCEPTANCE_BASE_URL;
const source = env.RETROM_DAPHNE_GAME_DIR;
const output = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/daphne-product");
mkdirSync(output, {recursive: true});
const progressPath = join(output, "progress.json");
const evidence = {caseId: "ACC-DAPHNE-001", status: "FAIL", stages: [], errors: [], content: []};
const tracePath = join(output, "browser-trace.ndjson");
writeFileSync(tracePath, "");
const trace = detail => appendFileSync(tracePath, JSON.stringify({atMs: Date.now(), ...detail}) + "\n");
const fatalBrowserError = message => !message.includes("This is not an error.") &&
  /querySelector|DAPHNE_WORKER_EXCEPTION|(?:pthread|worker|thread).*(?:error|crash|fail)|__emscripten_thread_crashed|^ErrorEvent$|WebGL.*(?:error|fail)/iu.test(message);
function assertNoFatalBrowserErrors(stage) {
  const fatal = evidence.errors.find(message => message !== "Failed to fetch") ??
    evidence.coreLog?.find(fatalBrowserError);
  if (fatal) {throw Error(`DAPHNE_${stage.toUpperCase()}_BROWSER_ERROR:${fatal}`);}
}
async function captureVideoFrame(page, canvas, path) {
  for (let attempt = 0; attempt < 10; attempt++) {
    const screenshot = await canvas.screenshot({path});
    const {data, info} = await sharp(screenshot).removeAlpha().raw().toBuffer({resolveWithObject: true});
    let brightPixels = 0;
    for (let i = 0; i < data.length; i += 3) {
      if (Math.max(data[i], data[i + 1], data[i + 2]) > 50) {brightPixels++;}
    }
    const fraction = brightPixels / (info.width * info.height);
    const entropy = (await sharp(screenshot).stats()).entropy;
    evidence.videoFrameBrightFraction = fraction;
    evidence.videoFrameEntropy = entropy;
    assertNoFatalBrowserErrors("preview");
    if (fraction > 0.01 && entropy > 1.2) {return;}
    await page.waitForTimeout(2000);
  }
  throw Error(`DAPHNE_VIDEO_FRAME_BLANK:${evidence.videoFrameBrightFraction}:${evidence.videoFrameEntropy}`);
}
async function interstellarOverlay(canvas, path) {
  const screenshot = await canvas.screenshot({path});
  const {data, info} = await sharp(screenshot).removeAlpha().raw().toBuffer({resolveWithObject: true});
  assert.equal(info.width, 1280, "DAPHNE_GAMEPLAY_VIEWPORT_CHANGED");
  assert.equal(info.height, 900, "DAPHNE_GAMEPLAY_VIEWPORT_CHANGED");
  let gameOverPixels = 0;
  // Interstellar draws GAME OVER in a fixed opaque blue overlay above the video.
  for (let y = 380; y < 430; y++) {
    for (let x = 480; x < 850; x++) {
      const offset = (y * info.width + x) * 3;
      if (Math.abs(data[offset] - 99) <= 2 && Math.abs(data[offset + 1] - 158) <= 2 &&
        Math.abs(data[offset + 2] - 173) <= 2) {gameOverPixels++;}
    }
  }
  let lifeIconPixels = 0;
  // Three small red ships appear in the lower-left HUD only after gameplay starts.
  for (let y = 800; y < 850; y++) {
    for (let x = 40; x < 150; x++) {
      const offset = (y * info.width + x) * 3;
      if (data[offset] > 150 && data[offset] > data[offset + 1] * 1.5 &&
        data[offset] > data[offset + 2] * 1.4) {lifeIconPixels++;}
    }
  }
  return {gameOverPixels, lifeIconPixels};
}
async function waitForGameOver(page, canvas, stage) {
  for (let attempt = 0; attempt < 45; attempt++) {
    const overlay = await interstellarOverlay(canvas, join(output, `${stage}-game-over.png`));
    if (overlay.gameOverPixels > 3000) {return overlay;}
    await page.waitForTimeout(1000);
  }
  throw Error(`DAPHNE_${stage.toUpperCase()}_GAME_OVER_NOT_OBSERVED`);
}
async function startGameFromGameOver(page, canvas, stage) {
  const before = await waitForGameOver(page, canvas, stage);
  await gamepad(page, 8, 300); // Select inserts a coin.
  await gamepad(page, 9, 300); // Start begins a credited game.
  let after = before;
  for (let attempt = 0; attempt < 15; attempt++) {
    await page.waitForTimeout(1000);
    after = await interstellarOverlay(canvas, join(output, `${stage}-after-start.png`));
    if (after.gameOverPixels < 1500 && after.lifeIconPixels > 100) {break;}
  }
  evidence[`${stage}Input`] = {before, after};
  assert.ok(after.gameOverPixels < 1500 && after.lifeIconPixels > 100,
    `DAPHNE_${stage.toUpperCase()}_START_STILL_GAME_OVER`);
}
async function playerShipX(canvas, path) {
  const screenshot = await canvas.screenshot({path});
  const {data, info} = await sharp(screenshot).removeAlpha().raw().toBuffer({resolveWithObject: true});
  let pixels = 0;
  let sumX = 0;
  for (let y = 700; y < 800; y++) {
    for (let x = 300; x < 1200; x++) {
      const offset = (y * info.width + x) * 3;
      if (data[offset] === 222 && data[offset + 1] === 0 && data[offset + 2] === 0) {
        pixels++;
        sumX += x;
      }
    }
  }
  return pixels > 700 ? sumX / pixels : null;
}
async function verifyGamepadDirection(page, canvas) {
  let before = null;
  for (let attempt = 0; attempt < 20 && before === null; attempt++) {
    await page.waitForTimeout(1000);
    before = await playerShipX(canvas, join(output, "product-ship-before.png"));
  }
  assert.ok(before !== null, "DAPHNE_PLAYER_SHIP_NOT_VISIBLE");
  await gamepad(page, 15, 700); // Standard gamepad D-pad right.
  await page.waitForTimeout(1000);
  const after = await playerShipX(canvas, join(output, "product-ship-after.png"));
  evidence.directionInput = {before, after};
  assert.ok(after !== null && after > before + 100, "DAPHNE_DPAD_DID_NOT_MOVE_SHIP");
}
async function verifyGameAudio(page, frame) {
  for (let attempt = 0; attempt < 15; attempt++) {
    await page.waitForTimeout(1000);
    const audio = await frame.evaluate(() => window.__retromDaphneAudio);
    evidence.gameAudio = audio;
    if (audio?.nonSilentBuffers > 0) {return;}
  }
  throw Error(`DAPHNE_AUDIO_SILENT:${JSON.stringify(evidence.gameAudio)}`);
}
let browser, proxy;

async function open(context, launch, stage) {
  const page = await context.newPage();
  page.on("pageerror", error => {
    evidence.errors.push(error.message.slice(0, 240));
    trace({stage, type: "pageerror", message: error.message.slice(0, 500), stack: error.stack?.slice(0, 1500)});
    if (env.RETROM_DAPHNE_DEBUG === "1") {
      for (const frame of page.frames()) {
        void frame.evaluate(() => {
          const canvas = window.EJS_emulator?.Module?.canvas;
          return canvas && {id: canvas.id, tag: canvas.tagName, transferred: canvas.controlTransferredOffscreen,
            width: canvas.width, height: canvas.height};
        }).then(canvas => {if (canvas) {trace({stage, type: "canvas-state", canvas});}}, () => {});
      }
    }
  });
  if (env.RETROM_DAPHNE_DEBUG === "1") {
    page.on("console", message => {
      const log = evidence.coreLog ??= [];
      if (log.length < 1000) {log.push(message.text().slice(0, 500));}
      trace({stage, type: "console", message: message.text().slice(0, 500)});
    });
  }
  page.on("response", response => {
    const request = response.request();
    if (!new URL(response.url()).pathname.endsWith(".m2v") || request.method() !== "GET") {return;}
    evidence.content.push({stage, status: response.status(), range: request.headers().range ?? null,
      length: Number(response.headers()["content-length"] ?? 0)});
    trace({stage, type: "video-response", status: response.status(), range: request.headers().range ?? null});
  });
  await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 120_000});
  const deadline = Date.now() + 90_000;
  while (Date.now() < deadline) {
    const alert = await Promise.race([page.locator("body").innerText().catch(() => ""),
      new Promise(resolve => setTimeout(() => resolve(""), 2000))]);
    const failure = alert.match(/\b(?:PLAYER|PROVIDER|RUNTIME|DAPHNE|EMULATORJS)_[A-Z0-9_]+\b/u);
    if (failure) {throw Error(`${stage}:${failure[0]}`);}
    assertNoFatalBrowserErrors(stage);
    for (const frame of page.frames()) {
      if (!await Promise.race([frame.evaluate(() => !!window.EJS_emulator?.gameManager && window.EJS_emulator.started).catch(() => false),
        new Promise(resolve => setTimeout(() => resolve(false), 2000))])) {continue;}
      const canvas = frame.locator("canvas").first();
      await canvas.waitFor({state: "visible", timeout: 10_000});
      await page.waitForTimeout(3000);
      const postStartText = await page.locator("body").innerText().catch(() => "");
      const postStartFailure = postStartText.match(/\b(?:PLAYER|PROVIDER|RUNTIME|DAPHNE|EMULATORJS)_[A-Z0-9_]+\b/u);
      if (postStartFailure) {throw Error(`${stage}:${postStartFailure[0]}`);}
      const coreLog = await frame.evaluate(() => {
        try {return new TextDecoder().decode(window.EJS_emulator.Module.FS.readFile("/daphne_log.txt"));}
        catch {return "";}
      });
      if (env.RETROM_DAPHNE_DEBUG === "1") {evidence.nativeLog = coreLog;}
      if (/Could not initialize|LDP-VLDP ERROR|failed to open/iu.test(coreLog)) {
        throw Error(`DAPHNE_CORE_LOAD_FAILED:${coreLog.slice(-400)}`);
      }
      assertNoFatalBrowserErrors(stage);
      if (env.RETROM_DAPHNE_DEBUG === "1") {
        evidence.filesystem = await frame.evaluate(() => {
          const emulator = window.EJS_emulator;
          const fs = emulator?.Module?.FS;
          const inspect = path => {
            try {const node = fs.lookupPath(path).node; return {size: node.usedBytes, mode: node.mode};}
            catch (error) {return String(error);}
          };
          return {fileName: emulator?.fileName, paths: Object.fromEntries(
            ["/roms", "/roms/interstellar.zip", "/framefile", "/framefile/interstellar.txt",
              "/framefile/interstellar.m2v", "/framefile/interstellar.ogg", "/framefile/interstellar.dat",
              "/daphne_log.txt", "/roms/../daphne_log.txt"]
              .map(path => [path, inspect(path)])),
            coreLog: (() => {try {return new TextDecoder().decode(fs.readFile("/daphne_log.txt")).slice(-12000);}
              catch {return null;}})()};
        });
      }
      const id = launch.launchId ?? launch.previewId;
      const config = await page.evaluate(async value => (await fetch(`/runtime/launches/${value}/config`)).json(), id);
      assert.equal(config.runtime.targetId, "daphne");
      assert.equal(config.runtime.capabilities.checkpoint, false);
      assert.equal(config.resources.find(item => item.role === "game")?.kind, "FILE_TREE");
      await canvas.screenshot({path: join(output, `${stage}.png`)});
      return {page, frame, canvas, config};
    }
    await page.waitForTimeout(250);
  }
  await page.screenshot({path: join(output, `${stage}-failed.png`)});
  throw Error(`DAPHNE_${stage.toUpperCase()}_TIMEOUT`);
}

try {
  assert.ok(base && source && env.RETROM_CHROME_EXECUTABLE && env.RETROM_ACCEPTANCE_USERNAME &&
    env.RETROM_ACCEPTANCE_PASSWORD, "DAPHNE_ACCEPTANCE_INPUT_REQUIRED");
  proxy = await px68kLocalProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await context.addInitScript(() => {
    window.__retromDaphneAudio = {startedBuffers: 0, nonSilentBuffers: 0, peak: 0};
    const originalStart = AudioBufferSourceNode.prototype.start;
    AudioBufferSourceNode.prototype.start = function (...args) {
      const result = originalStart.apply(this, args);
      const audio = window.__retromDaphneAudio;
      audio.startedBuffers++;
      if (this.buffer) {
        let bufferPeak = 0;
        for (let channel = 0; channel < this.buffer.numberOfChannels; channel++) {
          const samples = this.buffer.getChannelData(channel);
          for (let index = 0; index < samples.length; index += Math.max(1, Math.floor(samples.length / 1024))) {
            bufferPeak = Math.max(bufferPeak, Math.abs(samples[index]));
          }
        }
        audio.peak = Math.max(audio.peak, bufferPeak);
        if (bufferPeak > 0.001) {audio.nonSilentBuffers++;}
      }
      return result;
    };
  });
  if (env.RETROM_DAPHNE_DEBUG === "1") {
    await context.addInitScript(() => Object.defineProperty(window, "EJS_DEBUG_XX", {
      get: () => true, set: () => undefined, configurable: true,
    }));
  }
  await installVirtualStandardGamepad(context);
  const client = await fantasyClient(context, base);
  let {itemId, gameId} = existsSync(progressPath) ? JSON.parse(readFileSync(progressPath, "utf8")) : {};
  if (itemId && !gameId) {
    const existing = await client.json("GET", `/api/v1/admin/reviews/${itemId}`);
    if (!existing.validation.current) {itemId = undefined;}
  }
  if (!itemId) {
    await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
      headers: client.writeHeaders(), data: {}, expected: 200,
    });
    const directories = await client.json("GET", "/api/v1/admin/platform-instances?platformId=daphne&limit=100");
    const instance = directories.items.find(item => item.enabled && item.defaultCoreId === "daphne");
    assert.ok(instance, "DAPHNE_PLATFORM_MISSING");
    const uploadId = await client.upload(directoryFiles(source), "DIRECTORY", "PROJECT");
    evidence.stages.push("upload");
    const imported = await client.json("POST", "/api/v1/admin/imports", {
      headers: client.writeHeaders(), expected: 202,
      data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "DAPHNE_PROJECT", tagIds: []},
    });
    itemId = (await reviewForImport(client, imported.importJobId, {attempts: 1200, waitMs: 100})).itemId;
    writeFileSync(progressPath, JSON.stringify({itemId, gameId}));
  }
  evidence.itemId = itemId;
  if (!gameId) {
    evidence.stages.push("review");
    const snapshot = await client.raw("GET", `/api/v1/admin/reviews/${itemId}`);
    assert.equal(snapshot.status(), 200);
    const review = await snapshot.json();
    if (!review.metadata.title) {
      await client.json("PATCH", `/api/v1/admin/reviews/${itemId}`, {
        headers: {...client.writeHeaders(), "If-Match": snapshot.headers().etag}, expected: 200,
        data: {metadata: {title: "Interstellar"}, tagIds: []},
      });
    }
    const created = await previewCart(client, itemId);
    const preview = await open(context, created, "preview");
    await startGameFromGameOver(preview.page, preview.canvas, "preview");
    await captureVideoFrame(preview.page, preview.canvas, join(output, "preview-after-5s.png"));
    assertNoFatalBrowserErrors("preview");
    await captureOptionalReviewScreenshot(preview.page, created.previewId);
    await preview.page.close();
    gameId = (await approveCart(client, itemId)).gameId;
    writeFileSync(progressPath, JSON.stringify({itemId, gameId}));
    evidence.stages.push("preview-and-approval");
  } else {
    evidence.stages.push("reused-published-game");
  }
  evidence.gameId = gameId;
  const launch = await open(context, await launchCart(client, gameId), "product");
  await startGameFromGameOver(launch.page, launch.canvas, "product");
  await verifyGamepadDirection(launch.page, launch.canvas);
  await verifyGameAudio(launch.page, launch.frame);
  assertNoFatalBrowserErrors("product");
  await revealPreviewToolbar(launch.page);
  const noSaveStatus = await launch.page.getByText("不支持存档", {exact: true}).count();
  const saveButtons = launch.page.getByRole("button", {name: "创建存档", exact: true});
  const saveDisabled = await saveButtons.count() === 0 || await saveButtons.first().isDisabled();
  evidence.noSaveUI = {noSaveStatus, saveDisabled};
  assert.ok(noSaveStatus > 0 && saveDisabled, "DAPHNE_NO_SAVE_UI_INVALID");
  await launch.page.close();
  const ranges = evidence.content.filter(entry => entry.range && entry.status === 206);
  assert.ok(ranges.length > 0, "DAPHNE_VIDEO_RANGE_NOT_OBSERVED");
  assert.ok(ranges.reduce((sum, entry) => sum + entry.length, 0) < 390_083_309,
    "DAPHNE_VIDEO_FULLY_MATERIALIZED");
  evidence.stages.push("product-range-and-input");
  evidence.status = "AUTOMATED_PASS_REQUIRES_VISUAL_REVIEW";
} catch (error) {
  evidence.errorCode = error instanceof Error ? error.message : String(error);
  process.exitCode = 1;
} finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(output, "product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}
