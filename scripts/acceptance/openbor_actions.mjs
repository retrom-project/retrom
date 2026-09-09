import assert from "node:assert/strict";
import {menuRow, nativeState} from "./openbor_observation.mjs";
import {revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";
import {observeCheckpointUpload, readCheckpointMultipart} from "./rpgmaker_checkpoint_upload.mjs";

export async function pad(page, button, duration = 220, delay = 700) {
  for (const pressed of [true, false]) {
    await Promise.all(page.frames().map(frame => frame.evaluate(({button, pressed}) => {
      globalThis.__retromTestGamepad?.button(button, pressed);
    }, {button, pressed})));
    await page.waitForTimeout(pressed ? duration : delay);
  }
}

export async function enterRobo(page, canvas, restore = false) {
  for (let i = 0; i < 8; i++) {
    const row = await menuRow(canvas);
    if (row !== null && row > 0.575 && row < 0.625) {break;}
    await pad(page, 9, 220, 1800);
  }
  await pad(page, 9, 220, 1000);
  console.log("choose mode", await menuRow(canvas));
  if (restore) {
    await pad(page, 13); await pad(page, 9, 220, 1500);
    // Native load_saved_game selects the first valid nonzero saved slot.
    await pad(page, 9, 220, 1800);
  } else {
    await pad(page, 9, 220, 1500); await pad(page, 13); await pad(page, 9, 220, 1800);
  }
  for (let i = 0; i < 10; i++) {
    const state = await nativeState(page);
    if (state?.levels.length) {return state;}
    await pad(page, 9, 220, 1800);
  }
  throw Error("OPENBOR_GAMEPLAY_TIMEOUT:" + JSON.stringify(await nativeState(page)));
}

export async function saveAndExit(page, launchId) {
  await revealPreviewToolbar(page);
  const observer = await observeCheckpointUpload(page, launchId);
  try {
    const requestTask = page.waitForRequest(request => request.method() === "POST" &&
      new URL(request.url()).pathname === `/runtime/launches/${launchId}/save-states`, {timeout: 30000});
    await page.getByRole("button", {name: "返回并退出游戏", exact: true}).click();
    await page.getByRole("button", {name: "存档并退出", exact: true}).click();
    const request = await requestTask;
    const response = await request.response();
    assert.equal(response?.status(), 201, "OPENBOR_SAVE_FAILED");
    const {form} = await readCheckpointMultipart(request, await observer.take(request));
    const payload = form.get("payload");
    assert(payload instanceof Blob && payload.size > 0);
    return {receipt: await response.json(), bytes: Buffer.from(await payload.arrayBuffer())};
  } finally {await observer.close();}
}

export async function checkPause(page, canvas, output) {
  await pad(page, 9); // Native pause menu, with an independent keyboard cancel.
  await canvas.screenshot({path: `${output}/native-pause.png`});
  await canvas.press("Escape", {delay: 150});
  await page.waitForTimeout(300);
  await revealPreviewToolbar(page);
  await page.getByRole("button", {name: "暂停", exact: true}).click();
  await page.waitForTimeout(300);
  const paused = await nativeState(page); assert.equal(paused.paused, true);
  await page.waitForTimeout(1000);
  assert.equal((await nativeState(page)).frames, paused.frames, "OPENBOR_PAUSE_FRAMES_ADVANCED");
  await canvas.screenshot({path: `${output}/paused.png`});
  await page.getByRole("button", {name: "继续游戏", exact: true}).click(); await canvas.click();
  return {frame: paused.frames};
}

export async function advanceToNativeSave(page, canvas, output) {
  // Ordinary native cheat-menu controls expedite crossing the first unsaved level.
  await pad(page, 9); await canvas.press("F12", {delay: 150}); await page.waitForTimeout(500);
  for (let i = 0; i < 3; i++) await pad(page, 13);
  await pad(page, 9);
  for (let i = 0; i < 2; i++) await pad(page, 13);
  await pad(page, 9);
  await pad(page, 15);
  for (let i = 0; i < 3; i++) await pad(page, 13);
  await pad(page, 15);
  for (let i = 0; i < 3; i++) await pad(page, 13);
  await pad(page, 15);
  await canvas.screenshot({path: `${output}/native-test-options.png`});
  for (let i = 0; i < 4; i++) {await canvas.press("Escape", {delay: 150}); await page.waitForTimeout(400);}
  await canvas.screenshot({path: `${output}/after-options.png`});
  for (let i = 0; i < 64; i++) {
    const state = await nativeState(page); console.log("native advance", i, state);
    if (state.saveBytes > 0) return state;
    if (i < 8) {
      await pad(page, 15, 5000, 100);
    } else {
      await pad(page, i % 2 ? 15 : 14, 500, 100);
      await pad(page, 5, 150, 500);
      for (let hit = 0; hit < 6; hit++) await pad(page, 0, 120, 300);
      await pad(page, i % 4 < 2 ? 12 : 13, 300, 100);
      if (i % 4 === 3) await pad(page, 15, 2000, 100);
    }
    await pad(page, 0, 150, 100);
    await canvas.screenshot({path: `${output}/advance-latest.png`});
  }
  throw Error("OPENBOR_NATIVE_SAVE_BOUNDARY_TIMEOUT");
}
