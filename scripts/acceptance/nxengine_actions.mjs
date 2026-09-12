import assert from "node:assert/strict";
import {nativeState, pad} from "./nxengine_observation.mjs";
import {revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";

export async function sprite(canvas) {
  return canvas.evaluate(element => {
    const {data, width, height} = element.getContext("2d").getImageData(0, 0, element.width, element.height);
    let sum = 0, count = 0;
    for (let y = 90; y < Math.min(height, 180); y++) for (let x = 30; x < width - 30; x++) {
      const i = (y * width + x) * 4;
      if (data[i] > 245 && data[i + 1] >= 65 && data[i + 1] <= 85 && data[i + 2] >= 32 && data[i + 2] <= 82) {sum += x; count++;}
    }
    return {count, x: count ? sum / count : null, cave: data[0] > 20 && data[0] < 150};
  });
}
export async function enterGame(page, canvas) {
  await canvas.click(); await page.waitForTimeout(1800);
  // The native title selects New for empty storage and Load when a profile exists.
  await pad(page, 0, 100, 1000);
  for (let i = 0; i < 70; i++) {
    const position = await sprite(canvas);
    if (position.cave && position.count >= 8) {await page.waitForTimeout(800); return;}
    await pad(page, 0, 350, 250);
  }
  throw Error("NXENGINE_GAMEPLAY_TIMEOUT");
}
export async function saveAtStartPoint(page, canvas, output) {
  await moveTogether(page, [0, 15], 700);
  await moveTogether(page, [0, 15], 420);
  await canvas.screenshot({path: `${output}/save-point-approach.png`});
  await pad(page, 13);
  for (let i = 0; i < 8; i++) {
    const state = await nativeState(page);
    if (state.saves.length) {
      await canvas.screenshot({path: `${output}/native-save.png`}); return state;
    }
    await pad(page, 0, 120, 600);
  }
  throw Error("NXENGINE_NATIVE_SAVE_MISSING");
}
export async function moveTogether(page, buttons, duration) {
  for (const pressed of [true, false]) {
    await Promise.all(page.frames().map(frame => frame.evaluate(({buttons, pressed}) => {
      for (const button of buttons) globalThis.__retromTestGamepad?.button(button, pressed);
    }, {buttons, pressed})));
    await page.waitForTimeout(pressed ? duration : 450);
  }
}
export async function saveAndExit(page, launchId) {
  await revealPreviewToolbar(page);
  const sent = page.waitForRequest(request => request.method() === "POST" &&
    new URL(request.url()).pathname === `/runtime/launches/${launchId}/save-states`, {timeout: 30000});
  await page.getByRole("button", {name: "返回并退出游戏", exact: true}).click();
  await page.getByRole("button", {name: "存档并退出", exact: true}).click();
  const request = await sent, response = await request.response();
  assert.equal(response?.status(), 201, "NXENGINE_SAVE_UPLOAD_FAILED");
  return {receipt: await response.json()};
}
export async function checkPause(page, canvas, output) {
  await revealPreviewToolbar(page); await page.getByRole("button", {name: "暂停", exact: true}).click();
  await page.waitForTimeout(250);
  const paused = await nativeState(page); await page.waitForTimeout(500);
  assert.equal((await nativeState(page)).frames, paused.frames, "NXENGINE_PAUSE_FRAMES_ADVANCED");
  await canvas.screenshot({path: `${output}/paused.png`});
  await page.getByRole("button", {name: "继续游戏", exact: true}).click(); await canvas.click();
  await page.waitForTimeout(300); assert((await nativeState(page)).frames > paused.frames);
  return {frame: paused.frames};
}
