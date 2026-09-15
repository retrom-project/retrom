import assert from "node:assert/strict";
import {test} from "node:test";
import sharp from "../../web/node_modules/sharp/dist/index.mjs";
import {keyboardBBKRPG, pictureBBKRPG} from "./bbkrpg_browser.mjs";

function screenshot(png) {return {canvas: {screenshot: async () => png}};}
function checkerboard() {
  const pixels = Buffer.alloc(159 * 96 * 3);
  for (let y = 0; y < 96; y++) {
    for (let x = 0; x < 159; x++) {
      const value = ((x >> 3) + (y >> 3)) % 2 ? 220 : 20;
      pixels.fill(value, (y * 159 + x) * 3, (y * 159 + x + 1) * 3);
    }
  }
  return sharp(pixels, {raw: {width: 159, height: 96, channels: 3}});
}

test("LCD observation ignores letterboxing around the same scaled frame", async () => {
  const native = await checkerboard().png().toBuffer();
  const expected = await pictureBBKRPG(screenshot(native), "", undefined);
  for (const [width, height] of [[1272, 900], [1600, 768]]) {
    const scaled = await checkerboard().resize(1272, 768, {kernel: "nearest"}).png().toBuffer();
    const boxed = await sharp({create: {width, height, channels: 3, background: "#05060a"}})
      .composite([{input: scaled, left: (width - 1272) / 2, top: (height - 768) / 2}]).png().toBuffer();
    const actual = await pictureBBKRPG(screenshot(boxed), "", undefined);
    assert.equal(actual.sha256, expected.sha256);
    assert.equal(actual.darkPixels, expected.darkPixels);
  }
});

test("an intro black frame is observable but can never be a ready menu", async () => {
  const png = await sharp({create: {width: 159, height: 96, channels: 3, background: "black"}}).png().toBuffer();
  const result = await pictureBBKRPG(screenshot(png), "", undefined);
  assert.equal(result.blank, true);
  assert.equal(result.selected, null);
});


// Model the frontend input contract: key events change a held-key state,
// and the emulated controller samples that state once each 60 Hz frame.
test("keyboard direction spans a controller poll and releases after the action", async () => {
  let time = 0, nextPoll = 1000 / 60, held = false, focused = false, observed = 0;
  const advance = milliseconds => {
    const end = time + milliseconds;
    while (nextPoll <= end) {
      if (held && focused) {observed++;}
      nextPoll += 1000 / 60;
    }
    time = end;
  };
  const opened = {
    page: {getByRole: () => ({isVisible: async () => false}), waitForTimeout: async value => advance(value)},
    canvas: {
      click: async () => {focused = true;},
      press: async (key, options) => {
        assert.equal(key, "w");
        held = true;
        advance(options?.delay ?? 0);
        held = false;
      },
    },
  };
  await keyboardBBKRPG(opened, "w");
  assert.ok(observed > 0, "keyboard press was lost between emulated input polls");
  assert.equal(held, false, "keyboard action must not leave a held direction");
});
