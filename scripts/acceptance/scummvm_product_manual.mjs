import assert from "node:assert/strict";
import {expect} from "../../web/node_modules/@playwright/test/index.mjs";
import {approveScummvm, importScummvm} from "./scummvm_product_api.mjs";
import {exitScummvm, gamepad, readyScummvm, scummvmFrame, skyGamepadProof, skyScene} from "./scummvm_product_controls.mjs";
import {scummvmSaveProof} from "./scummvm_product_save_proof.mjs";

export async function manualScummvm(context, client, archive, directory) {
  const review = await importScummvm(client, archive);
  const {gameId} = await approveScummvm(client, review.itemId);
  const page = await context.newPage();
  await page.goto(`/games/${gameId}`);
  await page.getByRole("button", {name: "开始游戏", exact: true}).click();
  await page.waitForURL(/\/play\//u); await readyScummvm(page); await skyScene(page);
  const originalLaunchId = new URL(page.url()).pathname.split("/").at(-1);
  await gamepad(page, {buttons: [2]}); await pointSky(page, 130, 56); await gamepad(page, {buttons: [0]});
  // Switching from a controller to text entry requires a real click into the canvas.
  const entry = await pointSky(page, 130, 20); await page.mouse.click(entry.x, entry.y);
  await page.keyboard.type("Manual save", {delay: 100});
  const entered = await scummvmFrame(page, directory, "manual-entered");
  await pointSky(page, 60, 156); await gamepad(page, {buttons: [0]});
  await expect(page.getByText("数据已暂存在此浏览器，退出时可保存", {exact: true})).toBeAttached({timeout: 15000});
  // Native Quit ends the engine before the Host can submit the frozen final file collection.
  await gamepad(page, {buttons: [3]}); await gamepad(page, {buttons: [2]});
  await pointSky(page, 130, 76); await gamepad(page, {buttons: [0]});
  await pointSky(page, 95, 96); await gamepad(page, {buttons: [0]});
  const dialog = page.getByRole("alertdialog", {name: "游戏已结束"});
  await expect(dialog).toBeVisible({timeout: 15000});
  await expect(page.getByRole("button", {name: "创建存档", exact: true})).toBeDisabled();
  await page.screenshot({path: `${directory}/manual-core-ended.png`});
  const response = page.waitForResponse((item) => item.request().method() === "POST" && item.url().endsWith("/save-states"), {timeout: 30_000});
  await dialog.getByRole("button", {name: "存档并退出", exact: true}).click();
  const savedResponse = await response; assert.equal(savedResponse.status(), 201);
  const saved = await savedResponse.json(); assert.equal(saved.resourceKind, "SAVE_STATE");
  await page.waitForURL(`/games/${gameId}`);
  await page.getByRole("button", {name: "▶ 从这里继续", exact: true}).click();
  await page.waitForURL(/\/play\//u); await readyScummvm(page);
  const restoredLaunchId = new URL(page.url()).pathname.split("/").at(-1);
  assert.notEqual(restoredLaunchId, originalLaunchId);
  const restore = await scummvmSaveProof(context, restoredLaunchId, false);
  assert(restore.fileCount >= 2, "SCUMMVM_NATIVE_DESCRIPTIONS_MISSING");
  await skyScene(page); await gamepad(page, {buttons: [2]});
  await pointSky(page, 130, 36); await gamepad(page, {buttons: [0]});
  const list = await scummvmFrame(page, directory, "manual-restored-list");
  await pointSky(page, 60, 156); await gamepad(page, {buttons: [0]});
  const restored = await scummvmFrame(page, directory, "manual-restored-game");
  assert.notEqual(list.rgbaSha256, restored.rgbaSha256, "SCUMMVM_MANUAL_LOAD_UNOBSERVED");
  const input = await skyGamepadProof(page, directory, "manual-restored");
  await exitScummvm(page); await page.waitForURL(`/games/${gameId}`); await page.close();
  return {itemId: review.itemId, gameId, originalLaunchId, restoredLaunchId, saveStateId: saved.saveStateId,
    restore, entered, list, restored, input, finalExportAfterNativeExit: true};
}

async function pointSky(page, x, y) {
  const bounds = await page.frameLocator("iframe").locator("canvas").boundingBox();
  const scale = Math.min(bounds.width / 320, bounds.height / 240);
  const point = {x: bounds.x + bounds.width / 2 + (x - 160) * scale,
    y: bounds.y + bounds.height / 2 + (y - 100) * scale * 1.2};
  await page.mouse.move(point.x, point.y);
  await page.waitForTimeout(250);
  return point;
}
