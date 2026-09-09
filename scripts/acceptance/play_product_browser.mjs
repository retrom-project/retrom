import assert from "node:assert/strict";
import {join} from "node:path";
import {writeFileSync} from "node:fs";
import {gamepad} from "./fantasy_product_client.mjs";
import {resumePreview, revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";

export async function observePlay(context) {
  await context.addInitScript(() => {
    Object.defineProperty(window, "__RETROM_PLAY_CORE_MODULE_V1__", {
      configurable: true,
      get() {return this.__playObservedModule;},
      set(value) {
        const create = value.createRetromPlay;
        value.createRetromPlay = async function(options) {
          const core = await create(options);
          window.__playAcceptanceFrames = () => core.frameCount();
          return core;
        };
        this.__playObservedModule = value;
      },
    });
  });
}

export async function openPlay(context, base, launch, evidence) {
  const page = await context.newPage();
  page.on("pageerror", error => evidence.errors.push(error.message));
  page.on("console", message => {
    if (message.text().includes("WebGL: INVALID")) {evidence.errors.push(message.text());}
  });
  page.on("response", response => {
    if (response.request().headers().range) {
      evidence.rangeRequests++; evidence.rangeBytes += Number(response.headers()["content-length"] ?? 0);
    }
  });
  await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 60000});
  const until = Date.now() + 90000;
  while (Date.now() < until) {
    for (const frame of page.frames()) {
      if (await frame.evaluate(() => typeof window.__playAcceptanceFrames === "function").catch(() => false)) {
        const canvas = frame.locator("#outputCanvas");
        await canvas.click({position: {x: 100, y: 100}});
        await waitForPlayPicture(page, canvas);
        evidence.launches.push(launch.launchId ?? launch.previewId);
        return {page, canvas, frame};
      }
    }
    assert.equal(await page.getByText("RUNTIME_FAILED", {exact: true}).isVisible(), false, "PLAY_RUNTIME_FAILED");
    await page.waitForTimeout(200);
  }
  throw Error("PLAY_READY_TIMEOUT");
}

async function waitForPlayPicture(page, canvas) {
  const until = Date.now() + 30000;
  while (Date.now() < until) {
    const visible = await canvas.evaluate(source => {
      const copy = document.createElement("canvas");
      copy.width = source.width; copy.height = source.height;
      const context = copy.getContext("2d"); context.drawImage(source, 0, 0);
      const pixels = context.getImageData(0, 0, copy.width, copy.height).data;
      let lit = 0;
      for (let index = 0; index < pixels.length; index += 4) {
        if (pixels[index] + pixels[index + 1] + pixels[index + 2] > 60) {lit++;}
      }
      return lit > 1000;
    });
    if (visible) {return;}
    await page.waitForTimeout(100);
  }
  throw Error("PLAY_PICTURE_TIMEOUT");
}

export async function playFrames(opened) {
  return opened.frame.evaluate(() => window.__playAcceptanceFrames());
}

export async function pausePlay(opened) {
  await revealPreviewToolbar(opened.page);
  await opened.page.getByRole("button", {name: "暂停", exact: true}).click();
}

export async function pressPlay(opened, button) {
  await resumePreview(opened.page);
  await opened.canvas.click({position: {x: 100, y: 100}});
  await gamepad(opened.page, button, 180);
  await opened.page.waitForTimeout(500);
}

export async function capturePlay(opened, directory, name) {
  const data = await opened.canvas.evaluate(canvas => canvas.toDataURL("image/png"));
  const bytes = Buffer.from(data.split(",")[1], "base64");
  assert.ok(bytes.length > 1024, "PLAY_SCREENSHOT_EMPTY");
  writeFileSync(join(directory, `${name}.png`), bytes);
}

export async function namePixels(opened) {
  return opened.canvas.evaluate(canvas => {
    const copy = document.createElement("canvas");
    copy.width = canvas.width; copy.height = canvas.height;
    const ctx = copy.getContext("2d"); ctx.drawImage(canvas, 0, 0);
    const pixels = ctx.getImageData(160, 145, 320, 40).data;
    let bright = 0;
    for (let i = 0; i < pixels.length; i += 4) {
      if (pixels[i] > 180 && pixels[i + 1] > 180 && pixels[i + 2] > 180) {bright++;}
    }
    return bright;
  });
}

export async function reachPlayFrame(opened, count) {
  const until = Date.now() + 90000;
  while (Date.now() < until) {
    if (await playFrames(opened) >= count) {return;}
    await opened.page.waitForTimeout(100);
  }
  throw Error("PLAY_FRAMES_TIMEOUT");
}
