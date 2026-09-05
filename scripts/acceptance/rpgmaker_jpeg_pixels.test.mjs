import assert from "node:assert/strict";
import {createRequire} from "node:module";
import test from "node:test";
import {jpegPixelsAsPng} from "./rpgmaker_jpeg_pixels.mjs";
const sharp = createRequire(new URL("../../web/package.json", import.meta.url))("sharp");
const image = (width, height) => sharp({
  create: {width, height, channels: 3, background: {r: 168, g: 85, b: 247}},
});

test("decodes an owned JPEG without resizing or changing its input bytes", async () => {
  const source = await image(320, 180).jpeg().toBuffer();
  const original = source.slice();
  const png = await jpegPixelsAsPng(source);
  assert.deepEqual(source, original);
  assert.deepEqual(png.subarray(0, 8), Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]));
  const {data, info} = await sharp(png).raw().toBuffer({resolveWithObject: true});
  assert.equal(info.width, 320);
  assert.equal(info.height, 180);
  assert.deepEqual(data, await sharp(source).raw().toBuffer());
});
test("rejects corruption, different image formats and dimensions outside the evidence bounds", async () => {
  const jpeg = await image(320, 180).jpeg().toBuffer();
  for (const bytes of [
    Buffer.alloc(0), Buffer.from([255, 216, 255, 0]), jpeg.subarray(0, -100),
    await image(320, 180).png().toBuffer(), await image(319, 180).jpeg().toBuffer(),
    await image(320, 179).jpeg().toBuffer(), await image(4097, 180).jpeg().toBuffer(),
  ]) {
    await assert.rejects(jpegPixelsAsPng(bytes), /RPG_ACCEPTANCE_RESTORE_SCREENSHOT_IMAGE_INVALID/);
  }
});
