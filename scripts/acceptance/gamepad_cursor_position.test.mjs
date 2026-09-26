import assert from "node:assert/strict";
import test from "node:test";
import {gamepadCursorDirection} from "./gamepad_cursor_position.mjs";

test("cursor targeting stops a settled axis while the other still needs movement", () => {
  assert.deepEqual(gamepadCursorDirection({x: 0.4, y: 0.36}, 0.08, 0.355), {x: -1, y: 0});
  assert.deepEqual(gamepadCursorDirection({x: 0.09, y: 0.5}, 0.08, 0.355), {x: 0, y: -1});
});

test("a centered cursor reaches the first KAG target within the existing 40 samples", () => {
  let position = {x: 0.5, y: 0.5};
  for (let sample = 0; sample < 40; sample++) {
    if (Math.abs(position.x - 0.08) <= 0.015 && Math.abs(position.y - 0.355) <= 0.015) {return;}
    const vector = gamepadCursorDirection(position, 0.08, 0.355);
    const length = Math.max(1, Math.hypot(vector.x, vector.y));
    position = {
      x: position.x + vector.x / length * 0.75 * 25 / 1440,
      y: position.y + vector.y / length * 0.75 * 25 / 810,
    };
  }
  assert.fail(`cursor did not reach the target: ${JSON.stringify(position)}`);
});
