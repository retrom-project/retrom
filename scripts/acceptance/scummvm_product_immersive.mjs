import {connectVirtualStandardGamepad} from "./standard_gamepad.mjs";
import assert from "node:assert/strict";
import {expect} from "../../web/node_modules/@playwright/test/index.mjs";
import {gamepad, readyScummvm, skyGamepadProof, skyScene} from "./scummvm_product_controls.mjs";

async function activate(page) {
  await page.bringToFront();
  await expect(page.locator('[data-immersive-shell="true"] > header > time[datetime]')).toBeVisible();
  await expect.poll(() => page.evaluate(() => document.hasFocus())).toBe(true);
  await connectVirtualStandardGamepad(page);
  // Let the hydrated input source observe the released controller before its first press.
  await gamepad(page, {});
  await gamepad(page, {buttons: [0]});
  await expect(page.locator('[data-immersive-shell="true"]')).toHaveAttribute("data-controller-state", "ready");
  await gamepad(page, {buttons: [0]});
  await page.waitForURL(/\/play\/.*experience=immersive/u);
  await readyScummvm(page);
  return new URL(page.url()).pathname.split("/").at(-1);
}

async function menu(page) {
  await gamepad(page, {buttons: [8, 9]}, 100);
  await gamepad(page, {buttons: [8, 9]}, 100);
  const dialog = page.getByRole("dialog", {name: "游戏菜单"});
  await expect(dialog).toBeVisible();
  return dialog;
}

async function select(page, dialog, label) {
  for (let i = 0; i < 3; i++) {
    if (await dialog.locator('button[aria-current="true"]').textContent() === label) {return;}
    await gamepad(page, {buttons: [15]});
  }
  throw new Error("SCUMMVM_IMMERSIVE_SELECTION_FAILED");
}

async function exit(page, dialog) {
  await select(page, dialog, "退出游戏"); await gamepad(page, {buttons: [0]});
  await expect(page.getByRole("alertdialog", {name: "退出游戏？"})).toBeVisible();
  await gamepad(page, {buttons: [15]}); await gamepad(page, {buttons: [0]});
  await page.waitForURL(/\/immersive\/library\//u);
}

export async function immersiveScummvm(context, gameId, directory) {
  const page = await context.newPage();
  try {
    await page.goto(`/immersive/library/all?gameId=${gameId}`);
    const originalLaunchId = await activate(page); await skyScene(page);
    const input = await skyGamepadProof(page, directory, "immersive-original");
    const dialog = await menu(page); await select(page, dialog, "创建存档");
    const response = page.waitForResponse((item) => item.request().method() === "POST" && item.url().endsWith("/save-states"), {timeout: 45_000});
    await gamepad(page, {buttons: [0]});
    const savedResponse = await response; assert.equal(savedResponse.status(), 201);
    const saved = await savedResponse.json(); await exit(page, dialog);
    await page.goto(`/immersive/library/saves?gameId=${gameId}&saveStateId=${saved.saveStateId}`);
    const restoredLaunchId = await activate(page); assert.notEqual(restoredLaunchId, originalLaunchId);
    const config = await (await context.request.get(`/runtime/launches/${restoredLaunchId}/config`)).json();
    assert.equal(config.restore?.format, "scummvm-save-bundle-v1-storage-v1");
    await connectVirtualStandardGamepad(page);
    const restoredInput = await skyGamepadProof(page, directory, "immersive-restored");
    await exit(page, await menu(page));
    return {originalLaunchId, restoredLaunchId, saveStateId: saved.saveStateId, input, restoredInput};
  } finally {await page.close();}
}
