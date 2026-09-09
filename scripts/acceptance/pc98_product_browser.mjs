import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {writeFileSync} from "node:fs";
import {join} from "node:path";
import {createRequire} from "node:module";
import {gamepad} from "./fantasy_product_client.mjs";
import {resumePreview, revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";

export async function observePC98(context) {
  await context.addInitScript(() => {
    globalThis.__pc98Audio = {buffers: 0, nonzeroBuffers: 0};
    const descriptor = Object.getOwnPropertyDescriptor(ScriptProcessorNode.prototype, "onaudioprocess");
    Object.defineProperty(ScriptProcessorNode.prototype, "onaudioprocess", {...descriptor,
      set(callback) {
        descriptor.set.call(this, callback && function(event) {
          callback.call(this, event);
          const evidence = globalThis.__pc98Audio;
          evidence.buffers++;
          if (event.outputBuffer.getChannelData(0).some(value => Math.abs(value) > 0.0001)) {evidence.nonzeroBuffers++;}
        });
      },
    });
    Object.defineProperty(window, "__RETROM_NP2KAI_FACTORY_V1__", {
      configurable: true, get() {return this.__pc98Factory;},
      set(factory) {
        this.__pc98Factory = async options => {
          const core = await factory(options);
          window.__pc98Frames = () => core._retrom_frame_count();
          return core;
        };
      },
    });
    globalThis.__pc98Progress = [];
    new MutationObserver(() => {
      const bar = document.querySelector('[role="progressbar"][aria-label="游戏内容加载进度"]');
      if (bar) {
        const value = Number(bar.getAttribute("aria-valuenow"));
        const values = globalThis.__pc98Progress;
        if (values.at(-1) !== value) {values.push(value);}
      }
    }).observe(document, {subtree: true, childList: true, attributes: true, attributeFilter: ["aria-valuenow"]});
  });
}

export async function openPC98(context, base, launch, evidence) {
  const page = await context.newPage();
  page.on("pageerror", error => evidence.errors.push(error.message.split("\n")[0].slice(0, 200)));
  const session = await context.newCDPSession(page);
  await session.send("Network.enable", {maxTotalBufferSize: 536870912, maxResourceBufferSize: 67108864});
  const requests = [];
  page.on("request", request => {if (request.method() === "GET") {requests.push(request.url());}});
  await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
  const deadline = Date.now() + 90000;
  while (Date.now() < deadline) {
    const failures = await page.getByRole("alert").allTextContents();
    const code = failures.join(" ").match(/\b(?:NP2KAI|RUNTIME|PLAYER)_[A-Z0-9_]+\b/u)?.[0];
    if (code) {throw Error(code);}
    for (const frame of page.frames()) {
      if (await frame.evaluate(() => typeof window.__pc98Frames === "function").catch(() => false)) {
        const canvas = frame.locator('canvas[aria-label="PC-98 game"]');
        await page.getByRole("status").filter({hasText: "可创建存档"}).waitFor({state: "attached", timeout: 45000});
        const config = await page.evaluate(async id => (await fetch(`/runtime/launches/${id}/config`)).json(), launch.launchId ?? launch.previewId);
        const disk = config.resources.find(resource => resource.kind === "ROM_BLOB");
        assert.equal(disk.sha256, evidence.disk.sha256);
        assert.equal(disk.sizeBytes, evidence.disk.sizeBytes);
        evidence.diskRequests += requests.filter(url => url === new URL(disk.url, base).href).length;
        evidence.runtimes.push({...config.runtime, runtimeBaseUrl: undefined, moduleUrl: undefined});
        return {page, frame, canvas, config};
      }
    }
    await page.waitForTimeout(100);
  }
  throw Error("PC98_READY_TIMEOUT");
}

export const framesPC98 = opened => opened.frame.evaluate(() => window.__pc98Frames());
export async function pressPC98(opened, button) {
  await resumePreview(opened.page);
  await opened.canvas.click();
  await gamepad(opened.page, button, 80);
  await opened.page.waitForTimeout(400);
}
export async function menuPC98(opened) {
  const deadline = Date.now() + 90000;
  while (await framesPC98(opened) < 1400) {
    assert.ok(Date.now() < deadline, "PC98_BOOT_TIMEOUT");
    await opened.page.waitForTimeout(200);
  }
  await pressPC98(opened, 0); // Title -> New Game menu.
  await pressPC98(opened, 0); // New Game -> opening selection map.
  await opened.page.waitForTimeout(1500);
  await pressPC98(opened, 1); // Escape -> RPG system menu.
  assert.ok((await picturePC98(opened)).blue > 10000, "PC98_GAME_MENU_MISSING");
}
export async function picturePC98(opened, directory, name) {
  const result = await opened.canvas.evaluate(canvas => {
    const copy = document.createElement("canvas"); copy.width = 640; copy.height = 400;
    const ctx = copy.getContext("2d"); ctx.drawImage(canvas, 0, 0);
    const all = ctx.getImageData(0, 0, 640, 400).data;
    let blue = 0, lit = 0, cursorY = null;
    for (let i = 0; i < all.length; i += 4) {
      if (all[i + 2] > 100 && all[i + 2] > all[i] * 2) {blue++;}
      if (all[i] + all[i + 1] + all[i + 2] > 60) {lit++;}
    }
    // The main-menu triangle blinks independently of the selected item.
    // Text starts at x=80; only the triangle occupies this inner left column.
    for (let y = 56; y < 176 && cursorY === null; y++) {
      for (let x = 64; x < 80; x++) {
        const i = (y * 640 + x) * 4;
        if (all[i] + all[i + 1] + all[i + 2] < 30) {cursorY = y; break;}
      }
    }
    return {blue, lit, cursorY, menu: Array.from(ctx.getImageData(48, 48, 120, 144).data), png: canvas.toDataURL()};
  });
  if (directory) {writeFileSync(join(directory, name + ".png"), Buffer.from(result.png.split(",")[1], "base64"));}
  return {blue: result.blue, lit: result.lit, cursorY: result.cursorY, menuSha256: createHash("sha256").update(Buffer.from(result.menu)).digest("hex")};
}
export async function visiblePC98Menu(opened, directory, name, sample = picturePC98) {
  const deadline = Date.now() + 5000;
  while (Date.now() < deadline) {
    const picture = await sample(opened, directory, name);
    if (picture.cursorY !== null) {return picture;}
    await opened.page.waitForTimeout(100);
  }
  throw Error("PC98_MENU_CURSOR_NOT_VISIBLE");
}
export async function pausePC98(opened) {
  await revealPreviewToolbar(opened.page);
  await opened.page.getByRole("button", {name: "暂停", exact: true}).click();
  const before = await framesPC98(opened);
  await opened.page.waitForTimeout(1500);
  assert.equal(await framesPC98(opened), before, "PC98_PAUSE_FAILED");
  return before;
}

export async function savePC98(opened, client, launchId, gameId, directory) {
  const path = `/api/v1/saves?gameId=${gameId}&limit=100`;
  const previous = new Set((await client.json("GET", path)).items.map(item => item.saveStateId));
  await revealPreviewToolbar(opened.page);
  const response = opened.page.waitForResponse(entry => entry.request().method() === "POST" &&
    new URL(entry.url()).pathname === `/runtime/launches/${launchId}/save-states`, {timeout: 60000});
  await opened.page.getByRole("button", {name: "创建存档", exact: true}).click();
  assert.equal((await response).status(), 201, "PC98_SAVE_HTTP_FAILED");
  // Chrome may evict a multipart request's response from its inspector cache.
  // Read the persisted result through the normal owner-filtered Saves API.
  const added = (await client.json("GET", path)).items.filter(item => !previous.has(item.saveStateId));
  assert.equal(added.length, 1, "PC98_SAVE_RECEIPT_AMBIGUOUS");
  const saved = added[0];
  assert.ok(saved.sizeBytes > 0 && saved.sizeBytes <= 402653184, "PC98_SAVE_SIZE_INVALID");
  const screenshot = await client.raw("GET", saved.screenshotUrl);
  assert.equal(screenshot.status(), 200);
  const bytes = await screenshot.body();
  const sharp = createRequire(new URL("../../web/package.json", import.meta.url))("sharp");
  const pixels = await sharp(bytes, {limitInputPixels: 640 * 400}).removeAlpha().raw().toBuffer();
  assert.ok(pixels.some(value => value > 100), "PC98_UPLOADED_SCREENSHOT_EMPTY");
  writeFileSync(join(directory, "uploaded-screenshot.png"), bytes);
  return saved;
}
