import assert from "node:assert/strict";
import {gamepad} from "./fantasy_product_client.mjs";
import {resumePreview} from "./rpgmaker_preview_actions.mjs";

export async function samDisk(opened) {
  return opened.frame.evaluate(async () => {
    const bytes = globalThis.Module.FS.readFile("/media/game.dsk");
    const digest = await crypto.subtle.digest("SHA-256", bytes);
    return {sizeBytes: bytes.length, sha256: Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, "0")).join("")};
  });
}

export async function samCommand(opened, text) {
  await resumePreview(opened.page); await opened.canvas.focus();
  await opened.page.keyboard.type(text, {delay: 300});
  await gamepad(opened.page, 0, 450); // Standard confirm reaches SAM's actual Return key.
  await opened.page.waitForTimeout(500);
}

export async function writeSamProgram(opened) {
  const before = await samDisk(opened);
  await samCommand(opened, "10 PRINT 1");
  await samCommand(opened, "SAVE CHR$ 82");
  let after = before;
  for (const deadline = Date.now() + 30000; Date.now() < deadline;) {
    after = await samDisk(opened);
    if (after.sha256 !== before.sha256) break;
    await opened.page.waitForTimeout(200);
  }
  assert.notEqual(after.sha256, before.sha256, "SAM_NATIVE_DISK_WRITE_MISSING");
  await opened.page.getByRole("status").filter({hasText: "数据已暂存在此浏览器"}).waitFor({state: "attached"});
  assert.equal(await opened.page.getByRole("button", {name: "创建存档", exact: true}).isEnabled(), false);
  return {before, after};
}

export async function samProgramOutput(opened) {
  await samCommand(opened, "CLS");
  const blank = await samOutputPixels(opened);
  await samCommand(opened, "RUN");
  const result = await samOutputPixels(opened);
  assert.notEqual(result.sha256, blank.sha256, "SAM_PROGRAM_DID_NOT_RENDER");
  assert.ok(result.litPixels > 0, "SAM_PROGRAM_OUTPUT_EMPTY");
  return result;
}

async function samOutputPixels(opened) {
  return opened.canvas.evaluate(async canvas => {
    const copy = document.createElement("canvas"); copy.width = canvas.width; copy.height = canvas.height;
    const context = copy.getContext("2d"); context.drawImage(canvas, 0, 0);
    // BASIC output, excluding the emulator's FPS overlay and blinking command cursor.
    const {data} = context.getImageData(0, Math.floor(copy.height * 0.09), Math.floor(copy.width * 0.9), Math.floor(copy.height * 0.6));
    const digest = await crypto.subtle.digest("SHA-256", data);
    let litPixels = 0;
    for (let offset = 0; offset < data.length; offset += 4) if (Math.max(data[offset], data[offset + 1], data[offset + 2]) > 100) litPixels++;
    return {sha256: Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, "0")).join(""), litPixels};
  });
}

// Safari Sam's blue trousers distinguish the player from the white-clothed enemies.
// Observe actual native pixels, excluding the FPS indicator and menus.
export async function safariPlayer(opened) {
  return opened.canvas.evaluate(canvas => {
    const copy = document.createElement("canvas"); copy.width = canvas.width; copy.height = canvas.height;
    const context = copy.getContext("2d"); context.drawImage(canvas, 0, 0);
    const {data, width, height} = context.getImageData(0, 0, copy.width, copy.height);
    let left = width, right = -1, top = height, bottom = -1, count = 0;
    for (let y = Math.floor(height / 4); y < height; y++) for (let x = 0; x < width; x++) {
      const offset = (y * width + x) * 4;
      if (data[offset] < 80 && data[offset + 1] < 100 && data[offset + 2] > 160) {
        left = Math.min(left, x); right = Math.max(right, x); top = Math.min(top, y); bottom = Math.max(bottom, y); count++;
      }
    }
    return {left, right, top, bottom, count, width, height};
  });
}

export async function bootSafari(opened) {
  await resumePreview(opened.page); await opened.canvas.focus();
  for (const deadline = Date.now() + 60000; Date.now() < deadline;) {
    const player = await safariPlayer(opened);
    if (player.count >= 5 && player.right - player.left < player.width / 8) return player;
    await gamepad(opened.page, 0, 150);
    await opened.page.waitForTimeout(700);
  }
  throw Error("SAM_SAFARI_GAME_TIMEOUT");
}

export async function moveSafari(opened, button = 15) {
  await resumePreview(opened.page); await opened.canvas.focus();
  // Native SAM F2 is the host numeric keypad's 2 (SimCoupe keyboard mapping).
  // The game's own F2 action selects cursor controls; the adapter mapping is unchanged.
  await opened.page.keyboard.down("Numpad2"); await opened.page.waitForTimeout(300); await opened.page.keyboard.up("Numpad2");
  const before = await safariPlayer(opened);
  assert.ok(before.count >= 5, "SAM_SAFARI_PLAYER_MISSING");
  await gamepad(opened.page, button, 500);
  const after = await safariPlayer(opened);
  assert.ok(after.count >= 5 && Math.abs(after.left - before.left) > 4, "SAM_SAFARI_PLAYER_DID_NOT_MOVE");
  return {button, before, after};
}
