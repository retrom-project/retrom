import assert from "node:assert/strict";
import {gamepad} from "./fantasy_product_client.mjs";
import {resumePreview} from "./rpgmaker_preview_actions.mjs";

// Observe the native checkpoint boundary. No emulated RAM, key map or restored bytes are changed.
export async function observeBBC(context) {
  await context.addInitScript(() => {
    const sha = async bytes => Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)), byte => byte.toString(16).padStart(2, "0")).join("");
    const decode = async bytes => {
      const text = await new Response(new Blob([bytes]).stream().pipeThrough(new DecompressionStream("gzip"))).text();
      const state = JSON.parse(text).state;
      const ram = Uint8Array.from(atob(state.ram.data), character => character.charCodeAt(0));
      const integers = new DataView(ram.buffer), at = letter => integers.getInt32(0x400 + (letter.charCodeAt(0) - 64) * 4, true);
      const screen = String.fromCharCode(...ram.subarray(0x7c00, 0x8000).map(value => value & 127));
      return {ramSha256: await sha(ram), welcomeVisible: screen.includes("Hello, this is the BBC"),
        titleVisible: screen.includes("BAT'N'BALL"), paddle: at("B"), paddleWidth: at("L"), ballX: at("Q"), ballY: at("R"),
        score: at("S"), ceiling: at("W"), xSpeed: at("U"), ySpeed: at("V")};
    };
    let bridge;
    globalThis.__bbcProduct = {captures: [], restores: [], read: async () => decode(await bridge.checkpoint())};
    Object.defineProperty(globalThis, "RetromJsbeeb", {configurable: true, get: () => bridge, set(original) {
      bridge = {...original,
        async checkpoint() {
          const bytes = await original.checkpoint();
          globalThis.__bbcProduct.captures.push({sha256: await sha(bytes), sizeBytes: bytes.length, state: await decode(bytes)});
          return bytes;
        },
        async restore(bytes) {
          await original.restore(bytes);
          const state = await decode(await original.checkpoint());
          globalThis.__bbcProduct.restores.push({sha256: await sha(bytes), sizeBytes: bytes.length, state});
        },
      };
    }});
  });
}

export async function bbcState(opened) {
  return opened.frame.evaluate(() => globalThis.__bbcProduct.read());
}

export async function bootBatBall(opened) {
  const {page, frame} = opened;
  await resumePreview(page);
  let ready = false;
  for (const deadline = Date.now() + 15000; Date.now() < deadline;) {
    if ((await bbcState(opened)).welcomeVisible) {ready = true; break;}
    await page.waitForTimeout(100);
  }
  assert.ok(ready, "BBC_WELCOME_DISK_NOT_READY");
  await page.keyboard.down("Escape"); await page.waitForTimeout(250); await page.keyboard.up("Escape");
  // This is jsbeeb's ordinary paste handler, which types the command through its native keyboard.
  await frame.evaluate(() => {
    const clipboardData = new DataTransfer(); clipboardData.setData("text/plain", 'CHAIN "W.BATBALL"\n');
    document.dispatchEvent(new ClipboardEvent("paste", {clipboardData, bubbles: true}));
  });
  let title = false;
  for (const deadline = Date.now() + 10000; Date.now() < deadline;) {
    if ((await bbcState(opened)).titleVisible) {title = true; break;}
    await page.waitForTimeout(150);
  }
  assert.ok(title, "BBC_BATBALL_TITLE_MISSING");
  for (const deadline = Date.now() + 45000; Date.now() < deadline;) {
    await gamepad(page, 9, 160); // jsbeeb maps standard Start to the game's SPACE confirmation.
    const state = await bbcState(opened);
    if (state.paddleWidth === 120 && state.ceiling === 700 && Math.abs(state.xSpeed) > 0 && state.ballY > 50) return state;
    await page.waitForTimeout(150);
  }
  throw Error("BBC_BATBALL_GAME_TIMEOUT");
}

export async function moveBatBall(opened, button = 15) {
  await resumePreview(opened.page);
  const before = await bbcState(opened);
  await gamepad(opened.page, button, 240);
  const after = await bbcState(opened);
  assert.equal(before.paddleWidth, 120, "BBC_GAME_NOT_RUNNING");
  assert.ok(button === 15 ? after.paddle > before.paddle : after.paddle < before.paddle, "BBC_GAMEPAD_PADDLE_UNCHANGED");
  return {button, before, after};
}
