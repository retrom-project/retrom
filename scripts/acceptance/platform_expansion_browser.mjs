import assert from "node:assert/strict";
import {writeFileSync} from "node:fs";
import {join} from "node:path";
import {createHash} from "node:crypto";
import {gamepad} from "./fantasy_product_client.mjs";
import {revealPreviewToolbar, resumePreview} from "./rpgmaker_preview_actions.mjs";

export async function observeExpansion(context) {
  await context.addInitScript(() => {
    window.__expansion = {inputs: [], restores: []};
    let onStart;
    const observed = new WeakSet();
    Object.defineProperty(window, "EJS_onGameStart", {configurable: true,
      get: () => onStart,
      set(callback) {
        if (typeof callback !== "function") {onStart = callback; return;}
        onStart = function(...args) {
          const manager = window.EJS_emulator.gameManager;
          if (!observed.has(manager)) {
            observed.add(manager);
            const input = manager.simulateInput;
            manager.simulateInput = function(...values) {
              window.__expansion.inputs.push(values);
              return input.apply(this, values);
            };
            const load = manager.loadExplicitStateAndWait;
            if (load) {
              manager.loadExplicitStateAndWait = async function(bytes, ...values) {
                await load.call(this, bytes, ...values);
                const digest = await crypto.subtle.digest("SHA-256", bytes);
                window.__expansion.restores.push({sizeBytes: bytes.length,
                  sha256: Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, "0")).join("")});
              };
            }
          }
          return callback.apply(this, args);
        };
      },
    });
  });
}

export async function openExpansion(context, base, launch, evidence) {
  const page = await context.newPage();
  page.on("pageerror", error => evidence.errors.push(error.message.split("\n")[0].slice(0, 180)));
  console.log(`${evidence.platform}: navigate`);
  await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
  console.log(`${evidence.platform}: document-loaded`);
  const deadline = Date.now() + 90000;
  while (Date.now() < deadline) {
    const errors = await bounded(page.locator("[role=alert]").allTextContents(), "ALERT_OBSERVATION_TIMEOUT", deadline - Date.now());
    const error = errors.join(" ").match(/\b(?:PLAYER|PROVIDER|RUNTIME)_[A-Z0-9_]+\b/u)?.[0];
    if (error) {throw Error(error);}
    for (const frame of page.frames()) {
      if (await bounded(frame.evaluate(() => !!window.EJS_emulator?.gameManager && window.EJS_emulator.started).catch(() => false), "FRAME_OBSERVATION_TIMEOUT", deadline - Date.now())) {
        await page.getByRole("status").filter({hasText: "可创建存档"}).waitFor({state: "attached", timeout: 45000});
        const id = launch.launchId ?? launch.previewId;
        const config = await page.evaluate(async value => (await fetch(`/runtime/launches/${value}/config`)).json(), id);
        evidence.runtimes.push({launchId: id, purpose: config.session.purpose, targetId: config.runtime.targetId,
          bundleSha256: config.runtime.bundleSha256, moduleSha256: config.runtime.moduleSha256,
          content: config.resources.filter(resource => resource.kind === "ROM_BLOB").map(({sha256, sizeBytes}) => ({sha256, sizeBytes})),
          restore: config.restore ? {format: config.restore.format, sha256: config.restore.sha256} : null});
        const canvas = frame.locator("canvas").first();
        await page.bringToFront(); await resumePreview(page); await canvas.click();
        return {page, frame, canvas, config};
      }
    }
    await page.waitForTimeout(100);
  }
  throw Error("PLATFORM_READY_TIMEOUT");
}

export async function pictureExpansion(opened, directory, name) {
  const bytes = await opened.canvas.screenshot();
  writeFileSync(join(directory, name + ".png"), bytes);
  return createHash("sha256").update(bytes).digest("hex");
}

export async function pressExpansion(opened, button, milliseconds = 120) {
  await resumePreview(opened.page);
  await opened.canvas.click();
  await gamepad(opened.page, button, milliseconds);
  await opened.page.waitForTimeout(250);
}

export async function holdExpansion(opened, button, pressed) {
  if (button === undefined) {return;}
  await Promise.all(opened.page.frames().map(frame => frame.evaluate(value => {
    globalThis.__retromTestGamepad?.button(value.button, value.pressed);
  }, {button: Number(button), pressed})));
}

export async function saveExpansion(opened, client, launch, gameId) {
  const path = `/api/v1/saves?gameId=${gameId}&limit=100`;
  const previous = new Set((await client.json("GET", path)).items.map(item => item.saveStateId));
  await revealPreviewToolbar(opened.page);
  await opened.page.getByRole("button", {name: "暂停", exact: true}).click();
  const before = await opened.frame.evaluate(() => window.EJS_emulator.gameManager.getFrameNum());
  await opened.page.waitForTimeout(300);
  assert.equal(await opened.frame.evaluate(() => window.EJS_emulator.gameManager.getFrameNum()), before);
  await revealPreviewToolbar(opened.page);
  const receipt = opened.page.waitForResponse(response => response.request().method() === "POST" &&
    new URL(response.url()).pathname === `/runtime/launches/${launch.launchId}/save-states`, {timeout: 45000});
  await opened.page.getByRole("button", {name: "创建存档", exact: true}).click();
  assert.equal((await receipt).status(), 201, "PLATFORM_SAVE_FAILED");
  const added = (await client.json("GET", path)).items.filter(item => !previous.has(item.saveStateId));
  assert.equal(added.length, 1, "PLATFORM_SAVE_MISSING");
  return added[0];
}

async function bounded(operation, code, milliseconds) {
  let timer;
  try {return await Promise.race([operation, new Promise((_, reject) => {timer = setTimeout(() => reject(Error(code)), Math.max(1, milliseconds));})]);}
  finally {clearTimeout(timer);}
}

export async function waitExpansionFrames(opened, count) {
  const start = await opened.frame.evaluate(() => window.EJS_emulator.gameManager.getFrameNum());
  await opened.frame.waitForFunction(target => window.EJS_emulator.gameManager.getFrameNum() >= target,
    start + count, {polling: 100, timeout: 90000});
  return {start, end: await opened.frame.evaluate(() => window.EJS_emulator.gameManager.getFrameNum())};
}
