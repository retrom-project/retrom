import assert from "node:assert/strict";
import {join} from "node:path";
import {gamepad} from "./fantasy_product_client.mjs";
import {resumePreview, revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";

export async function observePSP(context, evidence) {
  evidence.stoppedHosts = 0;
  await context.exposeBinding("__pspStopped", () => {evidence.stoppedHosts++;});
  await context.addInitScript(() => {
    Object.defineProperty(window, "__RETROM_PPSSPP_V1__", {
      configurable: true,
      get() {return this.__pspObservedModule;},
      set(value) {
        const create = value.createPPSSPPHost;
        value.createPPSSPPHost = async function(options) {
          const core = await create(options);
          window.__pspAcceptanceFrames = () => core.frameCount();
          const stop = core.stop;
          core.stop = async function() {await stop(); await window.__pspStopped();};
          return core;
        };
        this.__pspObservedModule = value;
      },
    });
  });
}
export async function openPSP(context, base, launch, evidence) {
  const page = await context.newPage();
  page.on("pageerror", error => evidence.errors.push(error.message));
  page.on("response", response => {
    if (response.status() >= 400) console.log("PSP_HTTP", response.status(), new URL(response.url()).pathname);
  });
  page.on("console", message => {
    if (message.type() === "error") console.log("PSP_BROWSER", message.text().slice(0, 500));
    if (/memory access out of bounds|WebGL: INVALID|PPSSPP_RESTORE_FAILED/u.test(message.text())) evidence.errors.push(message.text());
  });
  page.on("request", request => {
    if (/\/content\//u.test(request.url()) && !request.url().endsWith("index.json")) {
      evidence.contentRequests++; if (request.headers().range) evidence.rangeRequests++;
    }
  });
  await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
  const until = Date.now() + 120000;
  while (Date.now() < until) {
    for (const frame of page.frames()) {
      if (await frame.evaluate(() => typeof window.__pspAcceptanceFrames === "function").catch(() => false)) {
        const canvas = frame.locator("canvas"); await canvas.click({position: {x: 100, y: 100}});
        evidence.launches.push(launch.launchId ?? launch.previewId);
        const config = await page.evaluate(async launch => {
          const path = `/runtime/launches/${launch.launchId ?? launch.previewId}/config`;
          return (await fetch(path)).json();
        }, launch);
        assert.equal(config.runtime?.providerId, "retrom-runtime"); assert.equal(config.runtime?.targetId, "ppsspp");
        (evidence.runtimes ??= []).push({providerVersion: config.runtime.providerVersion,
          bundleSha256: config.runtime.bundleSha256, moduleSha256: config.runtime.moduleSha256});
        const opened = {page, canvas, frame, config}; await checkPSPLayout(opened); return opened;
      }
    }
    assert.equal(await page.getByText("RUNTIME_FAILED", {exact: true}).isVisible(), false, "PSP_RUNTIME_FAILED");
    await page.waitForTimeout(200);
  }
  await page.screenshot({path: join(process.env.RETROM_ACCEPTANCE_CASE_DIR, "startup-failed.png")});
  console.log("PSP_FAILED_UI", (await page.locator("body").innerText()).slice(0, 4000));
  throw Error("PSP_READY_TIMEOUT");
}
export async function checkPSPLayout(opened) {
  const layout = await opened.canvas.evaluate(canvas => {
    const box = canvas.getBoundingClientRect(), scale = Math.min(innerWidth / 480, innerHeight / 272);
    return {width: box.width, height: box.height, left: box.left, top: box.top,
      expectedWidth: Math.round(480 * scale), expectedHeight: Math.round(272 * scale),
      viewportWidth: innerWidth, viewportHeight: innerHeight};
  });
  assert.ok(Math.abs(layout.width - layout.expectedWidth) <= 1 && Math.abs(layout.height - layout.expectedHeight) <= 1,
    "PSP_VIEWPORT_NOT_FITTED");
  assert.ok(Math.abs(layout.left - (layout.viewportWidth - layout.width) / 2) <= 1 &&
    Math.abs(layout.top - (layout.viewportHeight - layout.height) / 2) <= 1, "PSP_VIEWPORT_NOT_CENTERED");
  return layout;
}
export async function capturePSP(opened, directory, name) {
  await opened.canvas.screenshot({path: join(directory, `${name}.png`)});
}
export async function pressPSP(opened, button) {
  await resumePreview(opened.page); await opened.canvas.click({position: {x: 100, y: 100}});
  await gamepad(opened.page, button, 180); await opened.page.waitForTimeout(500);
}
export async function pausePSP(opened) {
  await revealPreviewToolbar(opened.page);
  await opened.page.getByRole("button", {name: "暂停", exact: true}).click();
}
export async function pspFrames(opened) {return opened.frame.evaluate(() => window.__pspAcceptanceFrames());}
export async function skyMenu(opened) {
  return opened.canvas.evaluate(canvas => {
    const copy = document.createElement("canvas"); copy.width = 480; copy.height = 272;
    const context = copy.getContext("2d"); context.drawImage(canvas, 0, 0, 480, 272);
    const pixels = context.getImageData(0, 0, 480, 272).data, counts = [];
    for (let row = 0; row < 5; row++) {
      let count = 0;
      for (let y = Math.floor((0.54 + row * .075) * 272); y < (.60 + row * .075) * 272; y++) {
        for (let x = 41; x < 134; x++) {
          const index = (y * 480 + x) * 4;
          if (pixels[index] > 210 && pixels[index + 1] > 210 && pixels[index + 2] > 210) count++;
        }
      }
      counts.push(count);
    }
    const maximum = Math.max(...counts);
    return {selected: maximum > 30 ? counts.indexOf(maximum) : null, counts};
  });
}
export async function waitSkyMenu(opened) {
  // The initial PSP autosave notice uses Circle; each face button keeps one PSP mapping.
  await opened.page.waitForTimeout(8000); await pressPSP(opened, 1);
  // The splash also has white pixels in the menu area. Wait through its intro.
  await opened.page.waitForTimeout(30000);
  const until = Date.now() + 90000;
  while (Date.now() < until) {
    const menu = await skyMenu(opened);
    if (menu.selected !== null) return menu;
    await opened.page.waitForTimeout(1000);
  }
  throw Error("PSP_SKY_MENU_TIMEOUT");
}

export async function waitHalfMinuteMenu(opened) {
  await opened.page.waitForTimeout(30000); await pressPSP(opened, 9);
  await opened.page.waitForTimeout(15000); await pressPSP(opened, 9);
  await opened.page.waitForTimeout(3000);
  assert.equal(await halfMinuteSelection(opened), 0, "PSP_SECOND_MENU_MISSING");
}
export async function halfMinuteSelection(opened) {
  const observation = await opened.canvas.evaluate(canvas => {
    const copy = document.createElement("canvas"); copy.width = 480; copy.height = 272;
    const context = copy.getContext("2d"); context.drawImage(canvas, 0, 0, 480, 272);
    const data = context.getImageData(0, 0, 480, 272).data;
    const darkPanel = [[176, 78], [310, 78], [176, 129], [310, 129]].every(([x, y]) => {
      const i = (y * 480 + x) * 4;
      return data[i] < 50 && data[i + 1] < 50 && data[i + 2] < 90;
    });
    const counts = [82, 106].map(top => {
      let count = 0;
      for (let y = top; y < top + 16; y++) for (let x = 190; x < 201; x++) {
        const i = (y * 480 + x) * 4;
        if (data[i] > 220 && data[i + 1] > 220 && data[i + 2] > 220) count++;
      }
      return count;
    });
    return {darkPanel, counts};
  });
  return classifyHalfMinuteMenu(observation);
}
export function classifyHalfMinuteMenu({darkPanel, counts}) {
  if (!darkPanel) return null;
  return Math.max(...counts) > 35 && Math.min(...counts) < 10 ? counts.indexOf(Math.max(...counts)) : null;
}
export async function exitPSP(opened, launchId, evidence, returnUrl) {
  const stopped = evidence.stoppedHosts;
  await revealPreviewToolbar(opened.page);
  await opened.page.getByRole("button", {name: "返回并退出游戏", exact: true}).click();
  const finished = opened.page.waitForResponse(response => response.request().method() === "POST" &&
    new URL(response.url()).pathname === `/runtime/launches/${launchId}/finish`, {timeout: 30000});
  await opened.page.getByRole("alertdialog", {name: "退出游戏？"}).getByRole("button", {name: "退出游戏", exact: true}).click();
  assert.equal((await finished).status(), 200);
  await opened.page.waitForURL(returnUrl);
  assert.equal(evidence.stoppedHosts, stopped + 1, "PSP_EXIT_CLEANUP_MISSING");
  assert.equal(opened.page.frames().length, 1, "PSP_FRAME_LEAKED");
  assert.equal(opened.page.workers().length, 0, "PSP_WORKER_LEAKED");
  await opened.page.close();
}
