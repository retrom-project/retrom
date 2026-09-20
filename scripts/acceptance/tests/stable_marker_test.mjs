import assert from "node:assert/strict";
import test from "node:test";
import {stableMarker} from "../stable_marker.mjs";
test("CLS then CHPUT redraw must settle before a marker can prove movement or restored shape", async () => {
  let clock = 0, index = 0;
  const frames = [null, {x: 3, shape: "partial"}, {x: 4, shape: "whole"}, {x: 4, shape: "whole"}];
  const read = async () => {
    const frame = frames[index++];
    if (!frame) throw new Error("MSX_FIXTURE_MARKER_MISSING:0");
    return frame;
  };
  const wait = async ms => {clock += ms;};
  assert.deepEqual(await stableMarker(read, wait, () => clock), frames[3]);
  assert.equal(index, 4);
  await assert.rejects(stableMarker(async () => {throw new Error("MSX_FIXTURE_MARKER_MISSING:0");}, wait, () => clock), /UNSTABLE/);
  await assert.rejects(stableMarker(async () => {throw new Error("canvas detached");}, wait, () => clock), /canvas detached/);
});
