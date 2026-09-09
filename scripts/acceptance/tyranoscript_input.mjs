import assert from "node:assert/strict";

export async function sendTyranoGamepadInput(surface) {
  const result = await surface.evaluate(async () => {
    const observed = {gamepad: [], keyboard: []};
    const key = event => observed.keyboard.push({type: event.type, key: event.key});
    const pad = event => observed.gamepad.push(event.detail.button_name);
    document.addEventListener("keydown", key, true); document.addEventListener("keyup", key, true);
    window.TYRANO.kag.on("gamepad-pressdown.retrom-acceptance", pad);
    try {
      window.__retromTestGamepad.button(1, true);
      window.dispatchEvent(new Event("gamepadconnected"));
      const deadline = Date.now() + 10000;
      while (!observed.gamepad.length && !observed.keyboard.length && Date.now() < deadline) {
        await new Promise(resolve => setTimeout(resolve, 20));
      }
      window.__retromTestGamepad.button(1, false);
      await new Promise(resolve => setTimeout(resolve, 150));
      return observed;
    } finally {
      window.__retromTestGamepad.button(1, false);
      document.removeEventListener("keydown", key, true); document.removeEventListener("keyup", key, true);
      window.TYRANO.kag.off(".retrom-acceptance");
    }
  });
  assertTyranoSingleInput(result);
  return result;
}
export function assertTyranoSingleInput(result) {
  const native = result.gamepad.length === 1 && result.gamepad[0] === "B" && result.keyboard.length === 0;
  const legacy = result.gamepad.length === 0 && result.keyboard.length === 2 &&
    result.keyboard[0].type === "keydown" && result.keyboard[1].type === "keyup" &&
    result.keyboard.every(event => event.key === "Escape");
  assert.ok(native || legacy, "TYRANOSCRIPT_ACCEPTANCE_GAMEPAD_INPUT_UNOBSERVED");
}

export async function sendTyranoKeyboardInput(surface) {
  await surface.evaluate(() => {
    window.__retromKeyboardEvidence = [];
    window.__retromKeyboardObserver = event => window.__retromKeyboardEvidence.push({type: event.type, key: event.key});
    document.addEventListener("keydown", window.__retromKeyboardObserver, true);
    document.addEventListener("keyup", window.__retromKeyboardObserver, true);
  });
  try {
    await surface.evaluate(base => {base.tabIndex = 0; base.focus();});
    await surface.press("Escape", {delay: 120});
    const keyboard = await surface.evaluate(() => window.__retromKeyboardEvidence);
    assert.deepEqual(keyboard, [{type: "keydown", key: "Escape"}, {type: "keyup", key: "Escape"}],
      "TYRANOSCRIPT_ACCEPTANCE_KEYBOARD_INPUT_UNOBSERVED");
    return keyboard;
  } finally {
    await surface.evaluate(() => {
      document.removeEventListener("keydown", window.__retromKeyboardObserver, true);
      document.removeEventListener("keyup", window.__retromKeyboardObserver, true);
      delete window.__retromKeyboardObserver; delete window.__retromKeyboardEvidence;
    });
  }
}
