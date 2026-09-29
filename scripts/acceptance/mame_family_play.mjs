import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {gunzipSync} from "node:zlib";
import {join} from "node:path";
import {launchCart, gamepad, saveCart} from "./fantasy_product_client.mjs";
import {openFamily} from "./mame_family_client.mjs";
import {audioEvidence, verifyPause} from "./mame_product_support.mjs";
import {fighterPosition, waitForFighter, familyDisplay, familyScreenshot, shotEvidence} from "./mame_guntus_observe.mjs";

export async function playFamily(client, context, input, evidence, gameId) {
  const {directory, platform, profile} = input;
  const launch = await launchCart(client, gameId);
  const opened = await openFamily(context, launch, input, evidence, "product");
  evidence.runtime = opened.config.runtime;
  evidence.display = await familyDisplay(opened.page, opened.canvas, profile);
  await opened.canvas.screenshot({path: join(directory, "before-input.png")});
  await gamepad(opened.page, 0, 250);
  await opened.page.waitForTimeout(4500); // The game's opening jingle blocks its input loop.
  if (platform === "pv1000") {await gamepad(opened.page, 0, 250);}
  evidence.playerBefore = await waitForFighter(opened.canvas, platform);
  await opened.canvas.screenshot({path: join(directory, "after-confirm.png")});
  evidence.pause = await verifyPause(opened.page, opened.canvas);
  await gamepad(opened.page, 15, 500);
  evidence.playerRight = await fighterPosition(opened.canvas, platform);
  await opened.canvas.screenshot({path: join(directory, "after-right.png")});
  assert.ok(evidence.playerRight?.x > evidence.playerBefore.x + 5, "MAME_GUNTUS_PLAYER_DID_NOT_MOVE_RIGHT");
  await gamepad(opened.page, 0, 1000);
  await opened.canvas.screenshot({path: join(directory, "fire.png")});
  evidence.shot = await shotEvidence(opened.canvas, platform, evidence.playerRight);
  assert.ok(evidence.shot.longestVerticalRun >= 4, "MAME_GUNTUS_SHOT_NOT_VISIBLE");
  evidence.audio = await audioEvidence(opened.page);
  evidence.playerSaved = await fighterPosition(opened.canvas, platform);
  assert.ok(evidence.playerSaved, "MAME_GUNTUS_PLAYER_MISSING_AT_SAVE");
  const saved = await saveCart(opened.page, launch.launchId, profile.core);
  assert.equal(saved.checkpointFormat, "mame-state-v1-storage-v1");
  evidence.screenshots = await familyScreenshot(opened.page, profile.width);
  await opened.canvas.screenshot({path: join(directory, "saved.png")});
  await opened.page.close();
  evidence.stages.push("product-confirm-direction-audio-pause-save");
  console.log(`${platform}: saved`);
  await restoreFamily(client, context, input, evidence, gameId, launch, saved);
}

async function restoreFamily(client, context, input, evidence, gameId, original, saved) {
  const launch = await launchCart(client, gameId, saved.saveStateId);
  assert.notEqual(launch.launchId, original.launchId);
  const opened = await openFamily(context, launch, input, evidence, "restore");
  const restore = opened.config.restore;
  assert.equal(restore?.format, "mame-state-v1-storage-v1");
  const response = await client.raw("GET", restore.url);
  assert.equal(response.status(), 200);
  const packed = await response.body();
  assert.equal(packed.length, restore.sizeBytes);
  assert.equal(createHash("sha256").update(packed).digest("hex"), restore.sha256);
  const raw = gunzipSync(packed, {maxOutputLength: 64 * 1024 * 1024});
  assert.equal(raw.subarray(0, 8).toString(), "RTMAME01");
  evidence.checkpoint = {format: restore.format, packedBytes: packed.length, rawBytes: raw.length};
  evidence.playerRestored = await fighterPosition(opened.canvas, input.platform);
  await opened.canvas.screenshot({path: join(input.directory, "restored.png")});
  assert.ok(evidence.playerRestored && Math.abs(evidence.playerSaved.x - evidence.playerRestored.x) < 4 &&
    Math.abs(evidence.playerSaved.y - evidence.playerRestored.y) < 4, "MAME_GUNTUS_RESTORE_POSITION_CHANGED");
  await gamepad(opened.page, 14, 450);
  evidence.playerLeft = await fighterPosition(opened.canvas, input.platform);
  await opened.canvas.screenshot({path: join(input.directory, "restored-left.png")});
  assert.ok(evidence.playerLeft?.x < evidence.playerRestored.x - 5, "MAME_GUNTUS_RESTORED_INPUT_FAILED");
  await opened.page.close();
  evidence.stages.push("new-launch-restore-continued-direction");
}
