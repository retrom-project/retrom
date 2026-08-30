import assert from "node:assert/strict";
import test from "node:test";

import { compareKiriKiriVisualSamples } from "./kirikiri_visual_match.mjs";

test("matches a restored frame to B using only pixels that distinguish B from C", () => {
  const b = sample(160, [20, 30, 40, 255]);
  const c = sample(160, [180, 160, 140, 255]);
  const restored = sample(160, [24, 34, 44, 255]);

  const comparison = compareKiriKiriVisualSamples(b, c, restored);

  assert.equal(comparison.matched, true);
  assert.equal(comparison.discriminativePixelCount, 160);
  assert.ok(comparison.restoredToBMeanDistance < comparison.restoredToCMeanDistance / 2);
});

test("rejects a restored frame that is closer to C than B", () => {
  const b = sample(160, [20, 30, 40, 255]);
  const c = sample(160, [180, 160, 140, 255]);

  const comparison = compareKiriKiriVisualSamples(b, c, c);

  assert.equal(comparison.matched, false);
});

function sample(pixelCount, rgba) {
  return Array.from({ length: pixelCount }, () => rgba).flat();
}
