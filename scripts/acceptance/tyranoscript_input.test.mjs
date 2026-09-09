import assert from "node:assert/strict";
import {test} from "node:test";
import {assertTyranoSingleInput} from "./tyranoscript_input.mjs";
const keyboard = [{type: "keydown", key: "Escape"}, {type: "keyup", key: "Escape"}];
test("Tyrano acceptance rejects the old duplicate path while accepting one native or legacy target", () => {
  assert.throws(() => assertTyranoSingleInput({gamepad: ["B"], keyboard}));
  assert.throws(() => assertTyranoSingleInput({gamepad: [], keyboard: []}));
  assert.throws(() => assertTyranoSingleInput({gamepad: [], keyboard: keyboard.slice(0, 1)}));
  assertTyranoSingleInput({gamepad: ["B"], keyboard: []});
  assertTyranoSingleInput({gamepad: [], keyboard});
});
