import assert from "node:assert/strict";
import test from "node:test";
import {canvasImage} from "../canvas_image.mjs";
test("game pixels come from the native canvas bitmap, excluding overlaid Host controls", async () => {
  const bytes = Buffer.from("owned bitmap");
  const canvas = {
    evaluate: fn => fn({toDataURL: format => {
      assert.equal(format, "image/png"); return "data:image/png;base64," + bytes.toString("base64");
    }}),
    screenshot: () => {throw Error("Host overlays must not participate in game pixel assertions");},
  };
  assert.deepEqual(await canvasImage(canvas), bytes);
  await assert.rejects(canvasImage({evaluate: async () => "data:,"}), /PNG_REQUIRED/);
});
