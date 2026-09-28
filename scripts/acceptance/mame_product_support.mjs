import assert from "node:assert/strict";
import {canvasDigest} from "./px68k_product_support.mjs";
import {revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";

export async function observeAudio(context) {
  await context.addInitScript(() => {
    const samples = {buffers: 0, nonSilent: 0};
    globalThis.__mameAudioEvidence = samples;
    globalThis.__mameScreenshotEvidence = [];
    const toBlob = HTMLCanvasElement.prototype.toBlob;
    HTMLCanvasElement.prototype.toBlob = function (...args) {
      globalThis.__mameScreenshotEvidence.push({width: this.width, height: this.height});
      return Reflect.apply(toBlob, this, args);
    };
    const original = AudioBufferSourceNode.prototype.start;
    AudioBufferSourceNode.prototype.start = function (...args) {
      samples.buffers++;
      if (this.buffer?.getChannelData(0).some(value => Math.abs(value) > .0001)) {samples.nonSilent++;}
      return Reflect.apply(original, this, args);
    };
  });
}

export async function verifyDisplay(page, canvas) {
  const samples = [];
  for (const viewport of [{width: 1280, height: 900}, {width: 800, height: 600}, {width: 1280, height: 900}]) {
    await page.setViewportSize(viewport); await page.waitForTimeout(150);
    const size = await canvas.evaluate(element => {
      const rect = element.getBoundingClientRect();
      return {width: element.width, height: element.height, displayWidth: rect.width, displayHeight: rect.height};
    });
    assert.equal(size.width, 560); assert.equal(size.height, 192);
    assert.ok(Math.abs(size.displayWidth / size.displayHeight - 4 / 3) < .005, "MAME_DISPLAY_ASPECT_INVALID");
    assert.ok(size.displayWidth <= viewport.width && size.displayHeight <= viewport.height, "MAME_DISPLAY_OVERFLOW");
    samples.push({...viewport, canvas: size});
  }
  return samples;
}

export async function verifyPause(page, canvas) {
  await revealPreviewToolbar(page);
  await page.getByRole("button", {name: "暂停", exact: true}).click();
  const resume = page.getByRole("button", {name: "继续游戏", exact: true});
  await resume.waitFor({state: "visible"});
  const before = await canvasDigest(canvas);
  await page.waitForTimeout(500);
  assert.equal((await canvasDigest(canvas)).sha256, before.sha256, "MAME_PAUSE_DID_NOT_FREEZE");
  await resume.click(); await resume.waitFor({state: "hidden"});
  await canvas.click();
  return {frozenForMs: 500, resumed: true};
}

export async function screenshotEvidence(page) {
  const samples = (await Promise.all(page.frames().map(frame => frame.evaluate(() => globalThis.__mameScreenshotEvidence ?? [])))).flat();
  assert.ok(samples.some(value => value.width === 560 && value.height === 420), "MAME_SCREENSHOT_ASPECT_INVALID");
  return samples;
}
export async function audioEvidence(page) {
  const entries = await Promise.all(page.frames().map(frame => frame.evaluate(() => globalThis.__mameAudioEvidence)));
  const result = entries.reduce((total, value) => ({buffers: total.buffers + (value?.buffers ?? 0),
    nonSilent: total.nonSilent + (value?.nonSilent ?? 0)}), {buffers: 0, nonSilent: 0});
  assert.ok(result.nonSilent > 0, "MAME_AUDIO_SILENT");
  return result;
}

// Donkey Kong's first floor: orange cap pixels identify the player, excluding
// ladders, purple platforms, the hammer and the fixed fire at the far left. This case uses the named real disk.
export async function playerPosition(canvas) {
  return canvas.evaluate(element => {
    const {width, height} = element;
    if (width !== 560 || height !== 192) {return null;}
    const data = element.getContext("2d").getImageData(0, 0, width, height).data;
    let count = 0, sumX = 0, sumY = 0;
    for (let y = 140; y < 174; y++) {
      for (let x = 45; x < 200; x++) {
        const offset = (y * width + x) * 4;
        if (data[offset] > 150 && data[offset + 1] > 50 && data[offset + 1] < 200 && data[offset + 2] < 80) {
          count++; sumX += x; sumY += y;
        }
      }
    }
    return count >= 3 && count <= 200 ? {x: sumX / count, y: sumY / count, pixels: count} : null;
  });
}
export async function waitForPlayer(canvas) {
  for (let attempt = 0; attempt < 40; attempt++) {
    const position = await playerPosition(canvas);
    if (position) {return position;}
    await canvas.page().waitForTimeout(500);
  }
  throw Error("MAME_PLAYABLE_CHARACTER_NOT_FOUND");
}

export function checkConsole(warnings) {
  // The host deliberately uses same-origin frames for this declaration; Chrome
  // reports this sandbox advisory before any provider code executes.
  const known = "An iframe which has both allow-scripts and allow-same-origin for its sandbox attribute can escape its sandboxing.";
  assert.deepEqual(warnings.filter(entry => entry.type !== "warning" || entry.text !== known), [], "MAME_UNEXPLAINED_CONSOLE_MESSAGE");
}
