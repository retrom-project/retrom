import assert from "node:assert/strict";
import {mkdirSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {installVirtualStandardGamepad} from "./standard_gamepad.mjs";
import {fantasyClient, launchCart, gamepad} from "./fantasy_product_client.mjs";
import {resumePreview} from "./rpgmaker_preview_actions.mjs";
import {verifyPlayerSurfaceResume} from "../../web/e2e/player-pause-resume.ts";

const baseUrl = process.env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(process.env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/ruffle/layout");
mkdirSync(directory, {recursive: true});
const evidence = {caseId: "ACC-FLASH-001", check: "layout", status: "FAIL", samples: [], errors: []};
let browser, proxy, page;
try {
  assert.ok(baseUrl && process.env.RETROM_RUFFLE_EXTERNAL_GAME_ID, "RUFFLE_PROBE_INPUT_REQUIRED");
  proxy = await localRpgAcceptanceProxy(baseUrl);
  browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  evidence.browser = browser.version();
  const context = await browser.newContext({viewport: {width: 1280, height: 900},
    deviceScaleFactor: Number(process.env.RETROM_RUFFLE_DPR ?? 1), ...proxy.contextOptions});
  context.setDefaultTimeout(10000);
  await installVirtualStandardGamepad(context);
  const readiness = await context.request.get(`${baseUrl}/health/ready`);
  assert.equal(readiness.status(), 200);
  const client = await fantasyClient(context, baseUrl);
  page = await context.newPage();
  page.on("pageerror", (error) => evidence.errors.push(error.message));
  page.on("console", (message) => {if (message.type() === "error") {console.log("layout:console-error", message.text().slice(0, 300));}});
  if (process.env.RETROM_RUFFLE_LAUNCH_UI) {
    await page.goto(`${baseUrl}/games/${process.env.RETROM_RUFFLE_EXTERNAL_GAME_ID}`, {waitUntil: "domcontentloaded"});
    await page.waitForLoadState("load", {timeout: 30000});
    await page.waitForTimeout(1500);
    const created = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === "/api/v1/launches", {timeout: 30000});
    await page.getByRole("button", {name: "开始游戏", exact: true}).click();
    const response = await created; assert.equal(response.status(), 201);
    evidence.launchId = (await response.json()).launchId;
    await page.waitForURL("**/play/**");
    assert.equal(await page.evaluate(() => document.fullscreenElement !== null), true, "RUFFLE_AUTO_FULLSCREEN_FAILED");
  } else {
    const launch = await launchCart(client, process.env.RETROM_RUFFLE_EXTERNAL_GAME_ID);
    evidence.launchId = launch.launchId;
    await page.goto(`${baseUrl}${launch.playUrl}`, {waitUntil: "domcontentloaded"});
  }
  console.log("layout:page-loaded");
  await page.screenshot({path: join(directory, "loading.png"), timeout: 10000});
  let frame;
  for (const end = Date.now() + 60000; !frame && Date.now() < end;) {
    for (const entry of page.frames()) {
      const visible = await Promise.race([entry.locator("ruffle-player canvas").isVisible(),
        page.waitForTimeout(2000).then(() => false)]);
      if (visible) {frame = entry; break;}
    }
    if (!frame) {await page.waitForTimeout(100);}
  }
  assert.ok(frame, "RUFFLE_SURFACE_TIMEOUT");
  console.log("layout:canvas-visible");
  await frame.waitForFunction(() => document.querySelector("ruffle-player")?.ruffle().readyState === 2);
  const canvas = frame.locator("ruffle-player canvas");
  await page.waitForTimeout(1200);
  const notice = frame.locator("#hardware-acceleration-modal");
  if (await notice.isVisible()) {await notice.click({position: {x: 10, y: 100}}); evidence.softwareRendering = true;}
  await canvas.click({delay: 100, timeout: 5000}).catch(async (error) => {
    if (!await notice.isVisible()) {throw error;}
    await notice.click({position: {x: 10, y: 100}}); evidence.softwareRendering = true;
    await canvas.click({delay: 100});
  });
  await gamepad(page, 0);
  if (process.env.RETROM_PLAYER_PAUSE_RESUME) {
    await page.keyboard.press("Tab");
    await page.getByRole("button", {name: "调试信息", exact: true}).click();
    await verifyPlayerSurfaceResume(page, async (paused) => {
      const playing = await frame.evaluate(() => document.querySelector("ruffle-player").ruffle().isPlaying);
      assert.equal(playing, !paused, "RUFFLE_PAUSE_RESUME_STATE_MISMATCH");
    });
    await page.screenshot({path: join(directory, "surface-resumed-with-debug.png")});
    await page.getByRole("button", {name: "关闭调试信息面板"}).click();
    evidence.surfaceResume = "PASS";
  }
  for (let index = 0; index < 8; index++) {
    if (process.env.RETROM_RUFFLE_RESIZE) {await changeViewport(page, canvas, index);}
    const sample = await canvas.evaluate((element) => {
      const bounds = element.getBoundingClientRect();
      const api = element.getRootNode().host.ruffle();
      return {x: bounds.x, y: bounds.y, width: bounds.width, height: bounds.height,
        viewportWidth: innerWidth, viewportHeight: innerHeight, dpr: devicePixelRatio, style: element.style.cssText,
        pixelWidth: element.width, pixelHeight: element.height,
        metadata: api.metadata, scale: api.loadedConfig.scale, forceScale: api.loadedConfig.forceScale,
        forceAlign: api.loadedConfig.forceAlign};
    });
    sample.fullscreen = await page.evaluate(() => document.fullscreenElement !== null);
    evidence.samples.push(sample);
    sample.stableFrames = await canvas.evaluate(async (element) => {
      for (let count = 0; count < 12; count++) {
        await new Promise(requestAnimationFrame);
        const rect = element.getBoundingClientRect();
        if (rect.x !== 0 || rect.y !== 0 || rect.width !== innerWidth || rect.height !== innerHeight ||
          Math.abs(element.width - innerWidth * devicePixelRatio) > 1 ||
          Math.abs(element.height - innerHeight * devicePixelRatio) > 1) {throw Error("RUFFLE_FRAME_LAYOUT_OSCILLATION");}
      }
      return 12;
    });
    await page.screenshot({path: join(directory, `running-${index}.png`)});
    await page.waitForTimeout(400);
  }
  await frame.evaluate(() => document.querySelector("ruffle-player").ruffle().suspend());
  await canvas.screenshot({path: join(directory, "paused.png")});
  assert.ok(evidence.samples.every((sample) => sample.width === sample.viewportWidth && sample.height === sample.viewportHeight && sample.x === 0 && sample.y === 0), "RUFFLE_LAYOUT_UNSTABLE");
  assert.ok(evidence.samples.every((sample) => sample.forceScale && sample.forceAlign), "RUFFLE_LAYOUT_NOT_ENFORCED");
  assert.ok(evidence.samples.every((sample) => Math.abs(sample.pixelWidth - sample.width * sample.dpr) <= 1 &&
    Math.abs(sample.pixelHeight - sample.height * sample.dpr) <= 1), "RUFFLE_BACKING_BUFFER_MISMATCH");
  assert.deepEqual(evidence.errors, []);
  evidence.status = "PASS";
} catch (error) {
  evidence.errorCode = error.message; process.exitCode = 1;
  await page?.screenshot({path: join(directory, "failure.png"), timeout: 5000}).catch(() => undefined);
  evidence.failureText = await page?.locator("body").innerText({timeout: 5000}).catch(() => "");
} finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "ruffle-layout.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}

async function changeViewport(page, canvas, index) {
  if (index === 1 || index === 6) {
    const wasFullscreen = await page.evaluate(() => document.fullscreenElement !== null);
    await page.keyboard.press("Tab");
    const toggle = page.getByRole("button", {name: wasFullscreen ? "退出全屏" : "全屏", exact: true});
    await toggle.focus();
    await toggle.click();
    assert.equal(await page.evaluate(() => document.fullscreenElement !== null), !wasFullscreen, "RUFFLE_FULLSCREEN_TRANSITION_FAILED");
  }
  const viewports = {2: {width: 1920, height: 1080}, 3: {width: 900, height: 1280},
    4: {width: 1920, height: 1080}, 7: {width: 1280, height: 900}};
  if (viewports[index]) {await page.setViewportSize(viewports[index]);}
  if (index > 0) {await resumePreview(page); await canvas.click(); await page.waitForTimeout(600);}
}
