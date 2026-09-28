import assert from "node:assert/strict";
import {test} from "node:test";
import {visibleButterscotchFrame} from "../butterscotch_frame.mjs";

test("waits through a dark transition without weakening the pixel requirement", async () => {
  const dark = {width: 100, height: 100, nonBlackPixels: 9};
  const visible = {width: 100, height: 100, nonBlackPixels: 10};
  let captures = 0;
  let waits = 0;
  const result = await visibleButterscotchFrame(async () => ++captures === 1 ? dark : visible,
    {attempts: 3, pause: async () => {waits++;}});
  assert.equal(result, visible);
  assert.equal(captures, 2);
  assert.equal(waits, 1);
});

test("a persistently black game fails within the fixed capture budget", async () => {
  let captures = 0;
  await assert.rejects(visibleButterscotchFrame(async () => {
    captures++;
    return {width: 100, height: 100, nonBlackPixels: 0};
  }, {attempts: 3, pause: async () => {}}), /BUTTERSCOTCH_ACCEPTANCE_FRAME_UNAVAILABLE/u);
  assert.equal(captures, 3);
});

test("capture errors remain failures", async () => {
  await assert.rejects(visibleButterscotchFrame(async () => {throw new Error("lost canvas");}), /lost canvas/u);
});
