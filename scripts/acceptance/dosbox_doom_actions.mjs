import assert from "node:assert/strict";
import sharp from "../../web/node_modules/sharp/dist/index.cjs";
import {gamepad} from "./fantasy_product_client.mjs";
import {proofDigest} from "./content_io_case_proof.mjs";

// Observe the HUD and right-hand wall. The left doorway contains moving enemies;
// it cannot serve as an exact saved-view comparison.
export async function doomScene(opened) {
  const png = await opened.canvas.screenshot({animations: "disabled"});
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
    menu: region(96, 83, 125, 82), wall: region(210, 35, 60, 90)};
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
    // Doom II's initial HUD is 50 rounds and 100% health. A title/demo or a
    // difficulty overlay must not count as entering a fresh level.
    if (scene.ammo.redPixels === 210 && scene.health.redPixels === 344 && scene.menu.redPixels < 350) return {menu, scene};
    if (scene.menu.redPixels > 400) {await gamepad(opened.page, 9, 150); await opened.page.waitForTimeout(500);}
    else await opened.page.waitForTimeout(200);
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

export async function resumeDoom(opened) {
  // Observe real native frames, including the threaded core's pause boundary.
  await opened.page.waitForTimeout(100);
  const paused = await opened.frame.evaluate(() => EJS_emulator.gameManager.getFrameNum());
  await opened.page.waitForTimeout(200);
  const stillPaused = await opened.frame.evaluate(() => EJS_emulator.gameManager.getFrameNum());
  assert.equal(stillPaused, paused, "DOS_PAUSE_ADVANCED_EMULATION");
  await opened.page.getByRole("button", {name: "继续游戏", exact: true}).click();
  await opened.canvas.click();
  await opened.frame.waitForFunction(value => EJS_emulator.gameManager.getFrameNum() > value + 2, paused, {timeout: 5000});
  const resumed = await opened.frame.evaluate(() => EJS_emulator.gameManager.getFrameNum());
  return {paused, stillPaused, resumed};
}
