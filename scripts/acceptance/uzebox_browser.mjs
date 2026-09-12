import assert from "node:assert/strict";
import {gamepad} from "./fantasy_product_client.mjs";
import {resumePreview, revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";

export async function openUzebox(context, base, launch, evidence) {
  const page = await context.newPage();
  page.on("pageerror", error => evidence.errors.push(error.message.split("\n")[0].slice(0, 200)));
  const id = launch.launchId ?? launch.previewId;
  await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
  await page.locator(".player-loading").waitFor({state: "hidden", timeout: 90000});
  const canvas = page.frameLocator("iframe.player-frame").locator("canvas.ejs_canvas");
  await canvas.waitFor({state: "visible", timeout: 90000});
  await page.getByRole("status").filter({hasText: "可创建存档"}).waitFor({state: "attached", timeout: 45000});
  const frame = page.frames().find(item => item !== page.mainFrame());
  const config = await page.evaluate(async launchId => (await fetch(`/runtime/launches/${launchId}/config`)).json(), id);
  assert.equal(config.runtime.providerId, "emulatorjs");
  assert.equal(config.runtime.targetId, "uzem");
  const cart = config.resources.find(item => item.kind === "ROM_BLOB");
  assert.equal(cart.sha256, evidence.cart.sha256);
  assert.equal(cart.sizeBytes, evidence.cart.sizeBytes);
  const coreSha256 = await page.evaluate(async runtimeBase => {
    const response = await fetch(runtimeBase + "assets/4.2.3/data/cores/uzem-wasm.data");
    if (!response.ok) {throw Error("UZEBOX_CORE_FETCH_FAILED");}
    const digest = await crypto.subtle.digest("SHA-256", await response.arrayBuffer());
    return [...new Uint8Array(digest)].map(value => value.toString(16).padStart(2, "0")).join("");
  }, config.runtime.runtimeBaseUrl);
  assert.equal(coreSha256, process.env.RETROM_UZEBOX_CORE_SHA256);
  evidence.runtimes.push({id, coreSha256, ...config.runtime, runtimeBaseUrl: undefined, moduleUrl: undefined});
  await canvas.click();
  return {page, frame, canvas, config};
}
export async function pressUzebox(opened, button, duration = 150) {
  await resumePreview(opened.page);
  await opened.canvas.click();
  await gamepad(opened.page, button, duration);
}
export async function saveUzebox(opened, client, launchId, gameId) {
  const path = `/api/v1/saves?gameId=${gameId}&limit=100`;
  const before = new Set((await client.json("GET", path)).items.map(item => item.saveStateId));
  await revealPreviewToolbar(opened.page);
  const response = opened.page.waitForResponse(entry => entry.request().method() === "POST" &&
    new URL(entry.url()).pathname === `/runtime/launches/${launchId}/save-states`, {timeout: 60000});
  await opened.page.getByRole("button", {name: "创建存档", exact: true}).click();
  assert.equal((await response).status(), 201, "UZEBOX_SAVE_HTTP_FAILED");
  const added = (await client.json("GET", path)).items.filter(item => !before.has(item.saveStateId));
  assert.equal(added.length, 1, "UZEBOX_SAVE_RECEIPT_AMBIGUOUS");
  assert.ok(added[0].sizeBytes > 0 && added[0].sizeBytes <= opened.config.runtime.checkpoint.maxBytes);
  return added[0];
}
export async function observeUzeboxAudio(context) {
  await context.addInitScript(() => {
    window.__uzeboxAudio = {buffers: 0, nonzeroBuffers: 0};
    const start = AudioBufferSourceNode.prototype.start;
    AudioBufferSourceNode.prototype.start = function(...args) {
      if (this.buffer) {
        window.__uzeboxAudio.buffers++;
        if (this.buffer.getChannelData(0).some(value => Math.abs(value) > 0.0001)) {
          window.__uzeboxAudio.nonzeroBuffers++;
        }
      }
      return start.apply(this, args);
    };
  });
}
