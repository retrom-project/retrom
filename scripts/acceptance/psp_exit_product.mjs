import assert from "node:assert/strict";
import {mkdirSync, writeFileSync} from "node:fs";
import {resolve, join} from "node:path";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {localRpgAcceptanceProxy} from "./rpgmaker_local_proxy.mjs";
import {fantasyClient, launchCart} from "./fantasy_product_client.mjs";
import {waitForPreviewReady, revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const directory = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".artifacts/psp-exit");
mkdirSync(directory, {recursive: true});
const evidence = {caseId: "ACC-RUN-002", variant: "PSP_EXIT", status: "FAIL", events: [], errors: [], requests: []};
let browser, proxy;
try {
  assert.ok(base && env.RETROM_PSP_GAME_ID && env.RETROM_PSP_SAVE_ID && env.RETROM_CHROME_EXECUTABLE, "PSP_EXIT_INPUT_REQUIRED");
  proxy = await localRpgAcceptanceProxy(base);
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true,
    args: ["--autoplay-policy=no-user-gesture-required", "--use-angle=swiftshader", "--enable-unsafe-swiftshader"]});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}, ...proxy.contextOptions});
  await context.exposeBinding("__observePspExit", (_source, name) => evidence.events.push(name));
  const client = await fantasyClient(context, base);
  const savesPath = `/api/v1/saves?gameId=${env.RETROM_PSP_GAME_ID}&limit=100`;
  const beforeSaves = (await client.json("GET", savesPath)).items.map(item => item.saveStateId).sort();
  const launch = await launchCart(client, env.RETROM_PSP_GAME_ID, env.RETROM_PSP_SAVE_ID);
  evidence.launchId = launch.launchId;
  const page = await context.newPage();
  page.on("pageerror", error => evidence.errors.push(error.message.slice(0, 200)));
  await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded"});
  await waitForPreviewReady(page); await page.waitForTimeout(2000);
  const config = await page.evaluate(async id => (await fetch(`/runtime/launches/${id}/config`)).json(), launch.launchId);
  assert.equal(config.runtime.targetId, "ppsspp");
  evidence.runtime = {providerVersion: config.runtime.providerVersion, moduleSha256: config.runtime.moduleSha256,
    bundleSha256: config.runtime.bundleSha256};
  const frame = page.frames().find(candidate => candidate !== page.mainFrame());
  await frame.evaluate(() => {
    const emulator = window.EJS_emulator, manager = emulator.gameManager;
    const wrap = (target, key, event) => {
      const original = target[key];
      if (typeof original !== "function") {throw Error("PSP_EXIT_NATIVE_HOOK_MISSING:" + key);}
      target[key] = function(...args) {
        // callEvent also publishes save notifications during native cleanup.
        if (event !== "exit-event" || args[0] === "exit") {void window.__observePspExit(event);}
        return original.apply(this, args);
      };
    };
    wrap(manager.functions, "restart", "restart");
    wrap(manager.functions, "toggleMainLoop", "stop-loop");
    wrap(manager.FS, "unmount", "unmount");
    wrap(manager.Module, "abort", "abort");
    wrap(emulator, "callEvent", "exit-event");
  });
  await revealPreviewToolbar(page);
  await page.getByRole("button", {name: "全屏", exact: true}).click();
  await page.waitForFunction(() => document.fullscreenElement !== null);
  evidence.fullscreenBeforeExit = true;
  await revealPreviewToolbar(page);
  await page.getByRole("button", {name: "更多操作", exact: true}).click();
  await page.screenshot({path: join(directory, "more-menu.png")});
  await page.getByRole("menuitem", {name: "退出游戏", exact: true}).click();
  await page.getByRole("alertdialog", {name: "退出游戏？"}).waitFor();
  await page.screenshot({path: join(directory, "exit-confirm.png")});
  const session = await context.newCDPSession(page);
  const frames = [];
  session.on("Page.screencastFrame", event => {
    if (frames.length < 40) {frames.push(event.data);}
    void session.send("Page.screencastFrameAck", {sessionId: event.sessionId}).catch(() => undefined);
  });
  await session.send("Page.startScreencast", {format: "jpeg", quality: 80, maxWidth: 1280, maxHeight: 900});
  evidence.events.length = 0;
  page.on("request", request => {
    const path = new URL(request.url()).pathname;
    if (request.method() === "POST" && (path.startsWith("/runtime/launches/") || path === "/api/v1/launches")) {
      evidence.requests.push(path);
    }
  });
  await page.getByRole("alertdialog", {name: "退出游戏？"}).getByRole("button", {name: "退出游戏", exact: true}).click();
  await page.screenshot({path: join(directory, "exit-start.png")});
  await page.waitForURL(base + `/games/${env.RETROM_PSP_GAME_ID}`, {timeout: 15000});
  await page.screenshot({path: join(directory, "returned-to-game.png")});
  await session.send("Page.stopScreencast");
  frames.forEach((bytes, index) => writeFileSync(join(directory, `exit-frame-${index}.jpg`), Buffer.from(bytes, "base64")));
  evidence.capturedFrames = frames.length;
  assert.equal(evidence.events.filter(name => name === "restart").length, 0, "PSP_REBOOTED_DURING_EXIT");
  for (const name of ["exit-event", "unmount", "abort"]) {
    assert.equal(evidence.events.filter(event => event === name).length, 1, "PSP_EXIT_CLEANUP_MISSING:" + name);
  }
  assert.ok(evidence.events.includes("stop-loop"));
  assert.equal(page.frames().length, 1, "PSP_RUNTIME_FRAME_LEAKED");
  assert.ok(evidence.requests.includes(`/runtime/launches/${launch.launchId}/finish`));
  assert.ok(!evidence.requests.some(path => path.endsWith("/save-states") || path.endsWith("/start") || path === "/api/v1/launches"));
  assert.deepEqual((await client.json("GET", savesPath)).items.map(item => item.saveStateId).sort(), beforeSaves);
  assert.deepEqual(evidence.errors, []);
  evidence.status = "PASS";
} catch (error) {
  evidence.errorCode = error.message.split("\n")[0].slice(0, 300); process.exitCode = 1;
} finally {
  await browser?.close(); await proxy?.close();
  writeFileSync(join(directory, "psp-exit-product.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
}
