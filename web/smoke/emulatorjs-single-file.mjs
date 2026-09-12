import {createHash} from "node:crypto";
import {mkdir, readFile, writeFile} from "node:fs/promises";
import {resolve} from "node:path";
import {chromium} from "playwright";
import {requestValidatedLaunch} from "./emulatorjs-launch.mjs";

// Operator-only product smoke. The scenario contains existing, authorized game IDs;
// it never imports private ROMs into the ordinary automated test fixture set.
const scenarioPath = process.env.RETROM_SMOKE_SCENARIO;
const origin = process.env.RETROM_WEB_ORIGIN;
const username = process.env.RETROM_SMOKE_USERNAME;
const password = process.env.RETROM_SMOKE_PASSWORD;
const output = process.env.RETROM_SMOKE_OUTPUT;
if (!scenarioPath || !origin || !username || !password || !output) {
  throw new Error("SMOKE_SCENARIO_OR_ENVIRONMENT_REQUIRED");
}
const scenarios = JSON.parse(await readFile(scenarioPath, "utf8"));
const allowed = ["bsnes", "freeintv", "vecx", "fuse", "vice_x64sc", "gearcoleco", "virtualjaguar", "prboom", "vice_x128", "vice_xvic", "puae",
  "81", "cap32", "crocods", "vice_xpet", "vice_xplus4", "same_cdi", "vice_x64", "mednafen_pce"];
if (!Array.isArray(scenarios) || !scenarios.length || scenarios.some((item) =>
  !allowed.includes(item.coreId) || !/^[0-9a-f-]{36}$/u.test(item.gameId) ||
  !Array.isArray(item.beforeSave) || !Array.isArray(item.afterSave) || !Array.isArray(item.afterRestore))) {
  throw new Error("SMOKE_SCENARIO_INVALID");
}
for (const item of scenarios) {
  for (const value of [item.startupMs ?? 1000, item.restoreSettleMs ?? 1000]) {
    if (!Number.isInteger(value) || value < 0 || value > 90_000) {throw new Error("SMOKE_WAIT_INVALID");}
  }
  for (const options of [item.coreOptions, item.restoreCoreOptions]) {
    if (options && (typeof options !== "object" || Array.isArray(options) ||
      Object.entries(options).some(([key, value]) => !/^[A-Za-z0-9_]+$/u.test(key) || typeof value !== "string"))) {
      throw new Error("SMOKE_CORE_OPTIONS_INVALID");
    }
  }
}
await mkdir(output, {recursive: true});
const browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE,
  headless: process.env.RETROM_SMOKE_HEADED !== "1",
  args: ["--enable-unsafe-swiftshader"]});
try {
  for (const scenario of scenarios) {await runScenario(scenario);}
} finally {await browser.close();}

async function runScenario(scenario) {
  const context = await browser.newContext({baseURL: origin, viewport: {width: 1280, height: 800}});
  const evidence = {coreId: scenario.coreId, gameId: scenario.gameId, status: "RUNNING", sessions: []};
  const directory = resolve(output, scenario.coreId);
  await mkdir(directory, {recursive: true});
  let timer;
  try {
    await Promise.race([execute(context, scenario, directory, evidence), new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error("SMOKE_DEADLINE_EXCEEDED")), 240_000);
    })]);
    // Input and restored game position require review of this run's screenshots.
    // Frame/state hashes changing with elapsed time cannot prove gamepad response.
    evidence.status = "REVIEW_REQUIRED";
  } catch (error) {
    evidence.status = "FAILED";
    evidence.error = error.message;
    process.exitCode = 1;
  } finally {
    clearTimeout(timer);
    await context.close();
    evidence.completedAt = new Date().toISOString();
    await writeFile(resolve(directory, "result.json"), `${JSON.stringify(evidence, null, 2)}\n`);
    process.stdout.write(`${scenario.coreId}: ${evidence.status}\n`);
  }
}

async function execute(context, scenario, directory, evidence) {
  await installPad(context);
  const login = await context.request.post("/api/v1/auth/login", {
    data: {username, password}, headers: {Origin: origin},
  });
  if (!login.ok()) {throw new Error(`SMOKE_LOGIN_FAILED:${login.status()}`);}
  const csrf = (await login.json()).csrfToken;
  let page = await context.newPage();
  await launch(page, scenario, csrf, null, evidence);
  await configureCore(page, scenario);
  await page.waitForTimeout(scenario.startupMs ?? 1000);
  await page.screenshot({path: resolve(directory, "A-initial.png")});
  await inputs(page, scenario.beforeSave, directory, "before-save");
  const saved = await save(page);
  await page.screenshot({path: resolve(directory, "B-save.png")});
  await unobscuredScreenshot(page, resolve(directory, "B-save-game.png"));
  evidence.saveStateId = saved;
  const resume = page.getByRole("button", {name: "继续游戏", exact: true});
  if (await resume.isVisible()) {await resume.click();}
  await inputs(page, scenario.afterSave, directory, "after-save");
  await page.screenshot({path: resolve(directory, "C-later.png")});
  await exit(page);
  await page.close();
  page = await context.newPage();
  await launch(page, scenario, csrf, saved, evidence);
  // Native controller preferences are not part of a game's checkpoint. Apply
  // only explicitly requested restore settings, without resetting the game.
  await configureCore(page, {coreOptions: scenario.restoreCoreOptions});
  await page.waitForTimeout(scenario.restoreSettleMs ?? 1000);
  await page.screenshot({path: resolve(directory, "B-restored.png")});
  await inputs(page, scenario.afterRestore, directory, "after-restore");
  await exit(page);
}

async function configureCore(page, scenario) {
  if (!scenario.coreOptions) {return;}
  const frame = page.frames().find((item) => item !== page.mainFrame());
  // Exercise EmulatorJS's native settings path; ROM-specific settings remain
  // operator input and are never added to the host's production configuration.
  await frame.evaluate((options) => {
    for (const [key, value] of Object.entries(options)) {window.EJS_emulator.menuOptionChanged(key, value);}
  }, scenario.coreOptions);
  // Native options are consumed by retro_run before a reset can use them.
  await page.waitForTimeout(250);
  if (scenario.restartAfterOptions) {await frame.evaluate(() => window.EJS_emulator.gameManager.restart());}
}

async function unobscuredScreenshot(page, path) {
  // Keep the ordinary screenshot too. Only the host's pause label is hidden;
  // the emulated canvas and game state are unchanged.
  await page.screenshot({path, style: ".player-pause-overlay{visibility:hidden !important}"});
}

async function launch(page, scenario, csrf, saveStateId, evidence) {
  const created = await requestValidatedLaunch(() => page.request.post("/api/v1/launches", {
    data: {gameId: scenario.gameId, coreId: scenario.coreId, saveStateId, dosEntry: null,
      returnTo: `/games/${scenario.gameId}`,
      clientCapabilities: {secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true}},
    headers: {Origin: origin, "X-Retrom-Csrf": csrf, "Idempotency-Key": crypto.randomUUID()},
  }), (milliseconds) => page.waitForTimeout(milliseconds));
  const [configured] = await Promise.all([
    page.waitForResponse((item) => /\/runtime\/launches\/[^/]+\/config$/u.test(item.url())),
    page.goto(created.playUrl),
  ]);
  const config = await configured.json();
  const game = config.resources.filter((resource) => resource.role === "game");
  if (game.length !== 1 || game[0].kind !== "ROM_BLOB") {throw new Error("SMOKE_SINGLE_FILE_REQUIRED");}
  await page.locator(".player-loading").waitFor({state: "hidden", timeout: 90_000});
  await page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas").waitFor({timeout: 30_000});
  const checkpoint = await page.evaluate(() => window.__RETROM_E2E_RUNTIME_V1__.checkpoint());
  if (!checkpoint.sizeBytes || checkpoint.sizeBytes > config.runtime.checkpoint.maxBytes) {
    throw new Error("SMOKE_CHECKPOINT_INVALID");
  }
  if (saveStateId) {
    const state = await page.request.get(config.restore.url);
    const bytes = await state.body();
    if (!state.ok() || bytes.length !== config.restore.sizeBytes ||
      createHash("sha256").update(bytes).digest("hex") !== config.restore.sha256) {
      throw new Error("SMOKE_RESTORE_RESOURCE_INVALID");
    }
  }
  evidence.sessions.push({launchId: created.launchId, providerId: config.runtime.providerId,
    targetId: config.runtime.targetId, providerVersion: config.runtime.providerVersion,
    bundleSha256: config.runtime.bundleSha256, moduleSha256: config.runtime.moduleSha256,
    contentSha256: game[0].sha256, contentSizeBytes: game[0].sizeBytes, checkpoint,
    restore: config.restore ? {format: config.restore.format, sha256: config.restore.sha256,
      sizeBytes: config.restore.sizeBytes} : null});
}

async function installPad(context) {
  await context.addInitScript(() => {
    Object.defineProperty(Element.prototype, "requestFullscreen", {configurable: true, value: () => Promise.resolve()});
    const owner = window.top ?? window;
    owner.__retromE2EGamepads ??= [{axes: [0, 0, 0, 0], connected: true, index: 0,
      id: "Retrom Standard Test Pad 1", mapping: "standard", timestamp: 0,
      buttons: Array.from({length: 17}, () => ({pressed: false, touched: false, value: 0}))}];
    Object.defineProperty(navigator, "getGamepads", {configurable: true, value: () => owner.__retromE2EGamepads});
  });
}

function validateInputStep(step) {
    if (!Array.isArray(step.buttons) || step.buttons.some((button) => !Number.isInteger(button) || button < 0 || button > 16) ||
      !Number.isInteger(step.holdMs) || step.holdMs < 1 || step.holdMs > 10_000 ||
      !Number.isInteger(step.settleMs) || step.settleMs < 0 || step.settleMs > 30_000) {
      throw new Error("SMOKE_INPUT_INVALID");
    }
    if (step.keys !== undefined && (!Array.isArray(step.keys) || step.buttons.length ||
      step.keys.length > 4 || step.keys.some((key) => typeof key !== "string" || !/^[A-Za-z0-9 /]+$/u.test(key)))) {
      throw new Error("SMOKE_KEYBOARD_INPUT_INVALID");
    }
    return step.keys ?? [];
}

async function inputs(page, sequence, directory, prefix) {
  for (const [index, step] of sequence.entries()) {
    const keys = validateInputStep(step);
    if (keys.length) {
      await page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas").click();
      for (const key of keys) {await page.keyboard.down(key);}
    }
    await page.evaluate((buttons) => {
      const pad = window.__retromE2EGamepads[0];
      pad.buttons.forEach((button, i) => {
        button.pressed = buttons.includes(i); button.touched = button.pressed; button.value = button.pressed ? 1 : 0;
      });
      pad.timestamp += 1;
    }, step.buttons);
    await page.waitForTimeout(step.holdMs);
    for (const key of keys) {await page.keyboard.up(key);}
    await page.evaluate(() => {
      const pad = window.__retromE2EGamepads[0];
      pad.buttons.forEach((button) => {button.pressed = false; button.touched = false; button.value = 0;});
      pad.timestamp += 1;
    });
    await page.waitForTimeout(step.settleMs);
    await page.screenshot({path: resolve(directory, `${prefix}-${index}.png`)});
  }
}

async function save(page) {
  await page.mouse.move(20, 20);
  const pause = page.getByRole("button", {name: "暂停", exact: true});
  if (await pause.isVisible()) {await pause.click();}
  const [response] = await Promise.all([
    page.waitForResponse((item) => /\/save-states$/u.test(item.url()) && item.request().method() === "POST"),
    page.locator(".player-save-button").click(),
  ]);
  if (response.status() !== 201) {throw new Error(`SMOKE_SAVE_FAILED:${response.status()}`);}
  return (await response.json()).saveStateId;
}

async function exit(page) {
  await page.mouse.move(20, 20);
  await page.getByRole("button", {name: "返回并退出游戏"}).click();
  const [finished] = await Promise.all([
    page.waitForResponse((item) => /\/finish$/u.test(item.url()) && item.request().method() === "POST"),
    page.getByRole("alertdialog", {name: "退出游戏？"}).getByRole("button", {name: "退出游戏", exact: true}).click(),
  ]);
  if (!finished.ok()) {throw new Error("SMOKE_EXIT_FAILED");}
  await page.locator(".player-shell").waitFor({state: "detached"});
}
