import assert from "node:assert/strict";
import sharp from "../../web/node_modules/sharp/dist/index.cjs";
import {gamepad} from "./fantasy_product_client.mjs";
import {proofDigest} from "./content_io_case_proof.mjs";

// Observe fixed screen regions, not emulator input events or a changing full frame.
export async function doomScene(opened) {
  const png = await opened.canvas.screenshot();
  const {data, info} = await sharp(png).resize(320, 225, {kernel: "nearest"}).removeAlpha().raw().toBuffer({resolveWithObject: true});
  assert.equal(info.channels, 3);
  const region = (left, top, width, height) => {
    const bytes = Buffer.alloc(width * height * 3); let redPixels = 0;
    for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
      const source = ((top + y) * 320 + left + x) * 3, destination = (y * width + x) * 3;
      data.copy(bytes, destination, source, source + 3);
      if (data[source] > 110 && data[source + 1] < 50 && data[source + 2] < 50) redPixels++;
    }
    return {sha256: proofDigest(bytes), redPixels};
  };
  return {ammo: region(12, 182, 34, 19), health: region(49, 182, 55, 20),
    menu: region(96, 83, 125, 82), wall: region(65, 42, 70, 80)};
}

export async function bootDoom(opened) {
  let scene;
  const deadline = Date.now() + 45000;
  while (Date.now() < deadline) {
    scene = await doomScene(opened);
    if (scene.menu.redPixels > 400) break;
    await gamepad(opened.page, 9, 150); await opened.page.waitForTimeout(500);
  }
  assert.ok(scene.menu.redPixels > 400, "DOS_DOOM_MENU_NOT_OBSERVED");
  const menu = scene;
  await gamepad(opened.page, 9, 150); await opened.page.waitForTimeout(500);
  await gamepad(opened.page, 9, 150);
  while (Date.now() < deadline) {
    scene = await doomScene(opened);
    if (scene.ammo.redPixels > 60 && scene.health.redPixels > 100 && scene.menu.redPixels < 350) return {menu, scene};
    await opened.page.waitForTimeout(200);
  }
  throw Error("DOS_DOOM_LEVEL_NOT_OBSERVED");
}

export async function moveAndFireDoom(opened, direction = 15) {
  const before = await doomScene(opened);
  await gamepad(opened.page, direction, 400); await opened.page.waitForTimeout(200);
  const moved = await doomScene(opened);
  assert.notEqual(moved.wall.sha256, before.wall.sha256, "DOS_DOOM_VIEW_DID_NOT_TURN");
  await gamepad(opened.page, 0, 150); await opened.page.waitForTimeout(750);
  const fired = await doomScene(opened);
  assert.notEqual(fired.ammo.sha256, moved.ammo.sha256, "DOS_DOOM_AMMUNITION_DID_NOT_CHANGE");
  assert.ok(fired.ammo.redPixels > 30, "DOS_DOOM_HUD_MISSING");
  return {direction, action: 0, before, moved, fired};
}
