import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {writeFileSync} from "node:fs";
import {join} from "node:path";
import {createRequire} from "node:module";
import {gamepad} from "./fantasy_product_client.mjs";
import {resumePreview, revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";

export async function observeMini(context) {
  await context.addInitScript(() => {
    globalThis.__miniAudio = {buffers: 0, varyingBuffers: 0};
    const descriptor = Object.getOwnPropertyDescriptor(ScriptProcessorNode.prototype, "onaudioprocess");
    Object.defineProperty(ScriptProcessorNode.prototype, "onaudioprocess", {...descriptor,
      set(callback) {
        descriptor.set.call(this, callback && function(event) {
          callback.call(this, event);
          const evidence = globalThis.__miniAudio;
          evidence.buffers++;
          const samples = event.outputBuffer.getChannelData(0);
          if (samples.some(value => Math.abs(value - samples[0]) > 0.0001)) {evidence.varyingBuffers++;}
        });
      },
    });
    Object.defineProperty(window, "__RETROM_GBE_POKEMINI_FACTORY_V1__", {
      configurable: true, get() {return this.__miniFactory;},
      set(factory) {
        this.__miniFactory = async options => {
          const core = await factory(options);
          window.__miniFrames = () => core._retrom_frame_count();
          return core;
        };
      },
    });
    globalThis.__miniProgress = [];
    new MutationObserver(() => {
      const bar = document.querySelector('[role="progressbar"][aria-label="游戏内容加载进度"]');
      if (bar) {
        const value = Number(bar.getAttribute("aria-valuenow"));
        const values = globalThis.__miniProgress;
        if (values.at(-1) !== value) {values.push(value);}
      }
    }).observe(document, {subtree: true, childList: true, attributes: true, attributeFilter: ["aria-valuenow"]});
  });
}

export async function openMini(context, base, launch, evidence) {
  const page = await context.newPage();
  page.on("pageerror", error => evidence.errors.push(error.message.split("\n")[0].slice(0, 200)));
  const requests = [];
  page.on("request", request => {if (request.method() === "GET") {requests.push(request.url());}});
  await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
  const deadline = Date.now() + 90000;
  while (Date.now() < deadline) {
    const failures = await page.getByRole("alert").allTextContents();
    const code = failures.join(" ").match(/\b(?:GBE|RUNTIME|PLAYER)_[A-Z0-9_]+\b/u)?.[0];
    if (code) {throw Error(code);}
    for (const frame of page.frames()) {
      if (await Promise.race([frame.evaluate(() => typeof window.__miniFrames === "function"), new Promise(resolve => setTimeout(() => resolve(false), 5000))]).catch(() => false)) {
        const canvas = frame.locator('canvas[aria-label="Pokémon Mini game"]');
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
  throw Error("Mini_READY_TIMEOUT");
}

export const framesMini = opened => opened.frame.evaluate(() => window.__miniFrames());
export async function pressMini(opened, button) {
  await resumePreview(opened.page);
  await opened.canvas.click();
  await gamepad(opened.page, button, 250);
  await opened.page.waitForTimeout(1000);
}
export async function pictureMini(opened, directory, name) {
  const result = await opened.canvas.evaluate(canvas => {
    const copy = document.createElement("canvas"); copy.width = 96; copy.height = 64;
    const ctx = copy.getContext("2d"); ctx.drawImage(canvas, 0, 0, 96, 64);
    const all = ctx.getImageData(0, 0, 96, 64).data;
    return {pixels: Array.from(all), png: canvas.toDataURL()};
  });
  if (directory) {writeFileSync(join(directory, name + ".png"), Buffer.from(result.png.split(",")[1], "base64"));}
  return {sha256: createHash("sha256").update(Buffer.from(result.pixels)).digest("hex"),
    headerSha256: createHash("sha256").update(Buffer.from(result.pixels.slice(0, 96 * 14 * 4).filter((_, i) => i % 4 === 0).map(value => value < 160 ? 0 : 255))).digest("hex"),
    // The level menu animates the Pokémon thumbnail at (32,20)–(58,43).
    // Compare its static grid/cursor after restore while the guest keeps running.
    checkpointSha256: createHash("sha256").update(Buffer.from(result.pixels.filter((_, i) => i % 4 === 0).slice(96 * 14).map((value, i) => {
      const x = i % 96, y = Math.floor(i / 96) + 14;
      return x >= 32 && x <= 58 && y >= 20 && y <= 43 ? 255 : value < 160 ? 0 : 255;
    }))).digest("hex"),
    contentSha256: createHash("sha256").update(Buffer.from(result.pixels.slice(96 * 14 * 4).filter((_, i) => i % 4 === 0).map(value => value < 160 ? 0 : 255))).digest("hex"),
    colors: new Set(result.pixels.filter((_, i) => i % 4 === 0)).size};
}
export async function pauseMini(opened) {
  await revealPreviewToolbar(opened.page);
  await opened.page.getByRole("button", {name: "暂停", exact: true}).click();
  const before = await framesMini(opened);
  await opened.page.waitForTimeout(1500);
  assert.equal(await framesMini(opened), before, "Mini_PAUSE_FAILED");
  return before;
}

export async function saveMini(opened, client, launchId, gameId, directory) {
  const path = `/api/v1/saves?gameId=${gameId}&limit=100`;
  const previous = new Set((await client.json("GET", path)).items.map(item => item.saveStateId));
  await revealPreviewToolbar(opened.page);
  const response = opened.page.waitForResponse(entry => entry.request().method() === "POST" &&
    new URL(entry.url()).pathname === `/runtime/launches/${launchId}/save-states`, {timeout: 60000});
  await opened.page.getByRole("button", {name: "创建存档", exact: true}).click();
  assert.equal((await response).status(), 201, "Mini_SAVE_HTTP_FAILED");
  // Chrome may evict a multipart request's response from its inspector cache.
  // Read the persisted result through the normal owner-filtered Saves API.
  const added = (await client.json("GET", path)).items.filter(item => !previous.has(item.saveStateId));
  assert.equal(added.length, 1, "Mini_SAVE_RECEIPT_AMBIGUOUS");
  const saved = added[0];
  assert.ok(saved.sizeBytes > 0 && saved.sizeBytes <= 1048576, "Mini_SAVE_SIZE_INVALID");
  const screenshot = await client.raw("GET", saved.screenshotUrl);
  assert.equal(screenshot.status(), 200);
  const bytes = await screenshot.body();
  const sharp = createRequire(new URL("../../web/package.json", import.meta.url))("sharp");
  const pixels = await sharp(bytes, {limitInputPixels: 96 * 64}).removeAlpha().raw().toBuffer();
  assert.ok(pixels.some(value => value > 100), "Mini_UPLOADED_SCREENSHOT_EMPTY");
  writeFileSync(join(directory, "uploaded-screenshot.png"), bytes);
  return saved;
}
