import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {createRequire} from "node:module";
import {join} from "node:path";
import {expect} from "../../web/node_modules/@playwright/test/index.mjs";
const {PNG} = createRequire(import.meta.url)("../../web/node_modules/playwright-core/lib/utilsBundle.js");

export async function gamepad(page, {buttons = [], axes = [0, 0, 0, 0]}, milliseconds = 160) {
  const send = async (state) => {
    for (const frame of page.frames()) {
      await frame.evaluate(({buttons: pressed, axes: positions}) => {
        const pad = globalThis.__retromTestGamepad;
        for (let i = 0; i < 17; i++) {pad?.button(i, pressed.includes(i));}
        for (let i = 0; i < 4; i++) {pad?.axis(i, positions[i]);}
      }, state);
    }
  };
  await send({buttons, axes}); await page.waitForTimeout(milliseconds);
  await send({buttons: [], axes: [0, 0, 0, 0]}); await page.waitForTimeout(500);
}

export async function readyScummvm(page) {
  const canvas = page.frameLocator("iframe").locator("canvas");
  await expect(canvas).toBeVisible({timeout: 90_000});
  await expect.poll(() => page.evaluate(() => window.__RETROM_E2E_RUNTIME_V1__?.getState()), {timeout: 90_000}).toBe("RUNNING");
  return canvas;
}

export async function resumeScummvm(page) {
  if (await page.evaluate(() => window.__RETROM_E2E_RUNTIME_V1__?.getState()) === "PAUSED") {
    await page.getByRole("button", {name: "继续游戏", exact: true}).click();
  }
}

export async function skyScene(page) {
  await page.bringToFront(); await resumeScummvm(page);
  await page.waitForTimeout(7000);
  await page.frameLocator("iframe").locator("canvas").click();
  await page.keyboard.press("Escape"); await page.waitForTimeout(1000);
  await page.keyboard.press("Escape"); await page.waitForTimeout(1500);
}

export async function scummvmFrame(page, directory, name) {
  const canvas = page.frameLocator("iframe").locator("canvas");
  const layout = await canvas.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    return {width: element.width, height: element.height, displayWidth: rect.width, displayHeight: rect.height};
  });
  assert(layout.width >= 320 && layout.height >= 200 && layout.displayWidth >= 320 && layout.displayHeight >= 200);
  const bytes = await canvas.screenshot({path: join(directory, `${name}.png`)});
  const png = PNG.sync.read(bytes);
  let nonBlackPixels = 0;
  for (let i = 0; i < png.data.length; i += 4) {if (png.data[i] || png.data[i + 1] || png.data[i + 2]) {nonBlackPixels++;}}
  assert(nonBlackPixels > png.width * png.height / 100, "SCUMMVM_FRAME_EMPTY");
  return {rgbaSha256: createHash("sha256").update(png.data).digest("hex"), nonBlackPixels, ...layout};
}

export async function skyGamepadProof(page, directory, name) {
  await resumeScummvm(page);
  await gamepad(page, {buttons: [2]}); // X opens Sky's own static control panel.
  const before = await scummvmFrame(page, directory, `${name}-before-stick`);
  await gamepad(page, {axes: [1, 0, 0, 0]}, 350);
  const moved = await scummvmFrame(page, directory, `${name}-after-stick`);
  assert.notEqual(moved.rgbaSha256, before.rgbaSha256, "SCUMMVM_FIRST_STICK_MOVEMENT_UNOBSERVED");
  // Point at the native Restore control, then prove A activates it and Y closes it.
  const bounds = await page.frameLocator("iframe").locator("canvas").boundingBox();
  const scale = Math.min(bounds.width / 320, bounds.height / 240);
  await page.mouse.move(bounds.x + bounds.width / 2 - 30 * scale, bounds.y + bounds.height / 2 - 64 * scale * 1.2);
  const pointed = await scummvmFrame(page, directory, `${name}-pointed`);
  await gamepad(page, {buttons: [0]});
  const confirmed = await scummvmFrame(page, directory, `${name}-confirm`);
  assert.notEqual(confirmed.rgbaSha256, pointed.rgbaSha256, "SCUMMVM_CONFIRM_UNOBSERVED");
  await gamepad(page, {buttons: [3]});
  const cancelled = await scummvmFrame(page, directory, `${name}-cancel`);
  assert.notEqual(cancelled.rgbaSha256, confirmed.rgbaSha256, "SCUMMVM_CANCEL_UNOBSERVED");
  await gamepad(page, {buttons: [3]});
  return {before, moved, confirmed, cancelled};
}

export async function revealScummvmToolbar(page) {
  await page.mouse.move(page.viewportSize().width / 2, 2); await page.waitForTimeout(350);
}

export async function captureScummvm(page) {
  await revealScummvmToolbar(page);
  const response = page.waitForResponse((item) => item.request().method() === "POST" && item.url().endsWith("/save-states"), {timeout: 45_000});
  await page.getByRole("button", {name: "创建存档", exact: true}).click();
  const result = await response; assert.equal(result.status(), 201);
  return result.json();
}

export async function exitScummvm(page) {
  await revealScummvmToolbar(page);
  await page.getByRole("button", {name: "返回并退出游戏"}).click();
  const dialog = page.getByRole("alertdialog", {name: "退出游戏？"});
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", {name: /^(直接退出|继续退出)$/u}).click();
}
