import assert from "node:assert/strict";
import {test} from "node:test";
import {runInNewContext} from "node:vm";
import {installVirtualStandardGamepad, sendGamepadInput} from "./standard_gamepad.mjs";

test("virtual buttons preserve native prototype accessors and stable references", async () => {
  const realm = {navigator: {}, performance: {now: () => 10}};
  await installVirtualStandardGamepad({
    async addInitScript(callback) {runInNewContext(`(${callback.toString()})()`, realm);},
  });
  const button = realm.navigator.getGamepads()[0].buttons[0];
  assert.equal(Object.hasOwn(button, "pressed"), false);
  assert.equal(Object.hasOwn(button, "value"), false);
  assert.equal(button.pressed, false);
  realm.__retromTestGamepad.button(0, true);
  assert.equal(realm.navigator.getGamepads()[0].buttons[0], button);
  assert.equal(button.pressed, true);
  assert.equal(button.touched, true);
  assert.equal(button.value, 1);
  realm.__retromTestGamepad.button(0, false);
  assert.equal(button.value, 0);
});

test("physical input reaches parent and child realms without forcing focus", async () => {
  const changes = [[], []];
  const frames = changes.map((log) => ({
    async evaluate(callback, next) {
      runInNewContext(`(${callback.toString()})(next)`, {
        next,
        __retromTestGamepad: {
          axis: (index, value) => log.push({axis: index, value}),
          button: (index, pressed) => log.push({button: index, pressed}),
        },
      });
    },
  }));
  const page = {frames: () => frames, waitForTimeout: async () => {}};
  const canvas = {
    page: () => page,
    evaluate: async () => {assert.fail("input must not force focus or target only the canvas realm");},
  };
  await sendGamepadInput(canvas);
  for (const log of changes) {
    assert.deepEqual(log, [
      {axis: 1, value: 1}, {axis: 1, value: 0}, {button: 0, pressed: true}, {button: 0, pressed: false},
    ]);
  }
});
