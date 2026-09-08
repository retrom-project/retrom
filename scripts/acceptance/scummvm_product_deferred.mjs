import assert from "node:assert/strict";
import {approveScummvm, importScummvm, trackScummvmTraffic} from "./scummvm_product_api.mjs";
import {captureScummvm, exitScummvm, gamepad, readyScummvm, resumeScummvm, scummvmFrame} from "./scummvm_product_controls.mjs";
import {scummvmSaveProof} from "./scummvm_product_save_proof.mjs";

export async function deferredScummvm(context, client, archive, directory) {
  const trafficSets = [];
  const track = (page) => trafficSets.push(trackScummvmTraffic(page));
  context.on("page", track);
  const review = await importScummvm(client, archive);
  const reviewPage = await context.newPage(); await reviewPage.goto(`/admin/reviews/${review.itemId}`);
  const preview = await openPreview(reviewPage, "运行游戏");
  await comiScene(preview);
  const previewSaved = await captureScummvm(preview);
  assert.equal(previewSaved.resourceKind, "REVIEW_PREVIEW_CHECKPOINT");
  await exitScummvm(preview); await preview.waitForEvent("close").catch(() => assert(preview.isClosed()));
  const resumedPreview = await openPreview(reviewPage, "从试玩存档继续");
  const previewRestoreId = new URL(resumedPreview.url()).pathname.split("/").at(-1);
  assert.notEqual(previewRestoreId, previewSaved.previewId);
  const previewRestore = await scummvmSaveProof(context, previewRestoreId, true);
  await scummvmFrame(resumedPreview, directory, "deferred-preview-restored");
  await exitScummvm(resumedPreview);
  await resumedPreview.waitForEvent("close").catch(() => assert(resumedPreview.isClosed()));
  const {gameId} = await approveScummvm(client, review.itemId); await reviewPage.close();
  const page = await context.newPage();
  await page.goto(`/games/${gameId}`);
  await page.getByRole("button", {name: "开始游戏", exact: true}).click();
  await page.waitForURL(/\/play\//u); await readyScummvm(page); await comiScene(page);
  const originalLaunchId = new URL(page.url()).pathname.split("/").at(-1);
  const before = await scummvmFrame(page, directory, "deferred-original");
  const start = performance.now(); const saved = await captureScummvm(page);
  const captureMilliseconds = Math.round(performance.now() - start);
  assert.equal(saved.resourceKind, "SAVE_STATE");
  await exitScummvm(page); await page.waitForURL(`/games/${gameId}`);
  await page.getByRole("button", {name: "▶ 从这里继续", exact: true}).click();
  await page.waitForURL(/\/play\//u); await readyScummvm(page);
  const restoredLaunchId = new URL(page.url()).pathname.split("/").at(-1);
  assert.notEqual(restoredLaunchId, originalLaunchId);
  const restore = await scummvmSaveProof(context, restoredLaunchId, true);
  const restored = await scummvmFrame(page, directory, "deferred-restored");
  const input = await comiInput(page, directory);
  const plugins = trafficSets.flat().filter((item) => item.path.includes("/plugins/"));
  assert(plugins.length > 0 && plugins.every((item) => item.path.endsWith("/libscumm.so") && item.status === 206));
  await exitScummvm(page); await page.waitForURL(`/games/${gameId}`); await page.close();
  context.off("page", track);
  return {itemId: review.itemId, gameId, originalLaunchId, restoredLaunchId, saveStateId: saved.saveStateId,
    previewRestoreId, previewRestore, restore, captureMilliseconds, before, restored, input,
    selectedEngineId: "scumm", selectedPluginResponses: plugins.length};
}

async function openPreview(review, name) {
  const popup = review.waitForEvent("popup");
  await review.getByRole("button", {name, exact: true}).click();
  const page = await popup; await page.bringToFront(); await readyScummvm(page); return page;
}

async function comiScene(page) {
  await page.bringToFront(); await resumeScummvm(page);
  for (let i = 0; i < 5; i++) {
    await page.waitForTimeout(3000); await page.frameLocator("iframe").locator("canvas").focus();
    await page.keyboard.press("Escape");
  }
}

async function comiInput(page, directory) {
  await resumeScummvm(page); await gamepad(page, {buttons: [9]});
  const menu = await scummvmFrame(page, directory, "deferred-menu");
  await gamepad(page, {axes: [1, 0, 0, 0]}, 350);
  const moved = await scummvmFrame(page, directory, "deferred-menu-stick");
  assert.notEqual(menu.rgbaSha256, moved.rgbaSha256, "SCUMMVM_DEFERRED_RESTORE_INPUT_UNOBSERVED");
  const bounds = await page.frameLocator("iframe").locator("canvas").boundingBox();
  await page.mouse.move(bounds.x + bounds.width / 2, bounds.y + bounds.height * 0.418);
  const pointed = await scummvmFrame(page, directory, "deferred-menu-pointed");
  await gamepad(page, {buttons: [0]});
  const confirmed = await scummvmFrame(page, directory, "deferred-menu-confirm");
  assert.notEqual(pointed.rgbaSha256, confirmed.rgbaSha256, "SCUMMVM_DEFERRED_CONFIRM_UNOBSERVED");
  await gamepad(page, {buttons: [3]});
  const cancelled = await scummvmFrame(page, directory, "deferred-menu-cancel");
  assert.notEqual(confirmed.rgbaSha256, cancelled.rgbaSha256, "SCUMMVM_DEFERRED_CANCEL_UNOBSERVED");
  await gamepad(page, {buttons: [3]});
  return {menu, moved, confirmed, cancelled};
}
