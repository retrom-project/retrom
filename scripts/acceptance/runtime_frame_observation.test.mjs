import assert from "node:assert/strict";
import test from "node:test";
import {readRuntimeFrames} from "./runtime_frame_observation.mjs";

test("production frame evidence comes from the ordinary real counter and rejects unavailable text", async () => {
  let text = "4,210";
  const counter = {locator: () => counter, innerText: async () => text};
  const panel = {isVisible: async () => true, getByText: () => counter};
  const page = {evaluate: async () => null, getByRole: () => panel};
  assert.equal(await readRuntimeFrames(page), 4210);
  text = "不可用"; assert.equal(await readRuntimeFrames(page), null);
  text = "60.0 FPS"; assert.equal(await readRuntimeFrames(page), null);
});
