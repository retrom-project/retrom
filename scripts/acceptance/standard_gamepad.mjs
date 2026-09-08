export async function installVirtualStandardGamepad(context, {connected = true} = {}) {
  await context.addInitScript((initialConnected) => {
    const state = {
      connected: initialConnected,
      axes: [0, 0, 0, 0],
      values: Array(17).fill(0),
    };
    const buttons = state.values.map((_, index) => Object.create({
      get pressed() {return state.values[index] >= 0.5;},
      get touched() {return state.values[index] > 0;},
      get value() {return state.values[index];},
    }));
    Object.defineProperty(navigator, "getGamepads", {
      configurable: true,
      value: () => state.connected ? [{
        axes: state.axes, buttons, connected: true,
        id: "Retrom acceptance standard gamepad", index: 0, mapping: "standard", timestamp: performance.now(),
      }] : [null],
    });
    globalThis.__retromTestGamepad = {
      connected(value) {
        if (state.connected === value) {return;}
        const pad = {axes: state.axes, buttons, connected: value,
          id: "Retrom acceptance standard gamepad", index: 0, mapping: "standard", timestamp: performance.now()};
        state.connected = value;
        const event = new Event(value ? "gamepadconnected" : "gamepaddisconnected");
        Object.defineProperty(event, "gamepad", {value: pad});
        window.dispatchEvent(event);
      },
      axis(index, value) {state.axes[index] = value;},
      button(index, pressed) {state.values[index] = pressed ? 1 : 0;},
    };
  }, connected);
}

export async function sendGamepadInput(canvas) {
  const page = canvas.page();
  for (const input of [
    {axis: 1, value: 1}, {axis: 1, value: 0}, {button: 0, pressed: true}, {button: 0, pressed: false},
  ]) {
    // A physical controller is visible to every realm, including cores whose
    // module runs in the parent but presents an adopted canvas in a child.
    await Promise.all(page.frames().map((frame) => frame.evaluate((next) => {
      const gamepad = globalThis.__retromTestGamepad;
      if ("axis" in next) {gamepad?.axis(next.axis, next.value);}
      else {gamepad?.button(next.button, next.pressed);}
    }, input)));
    await page.waitForTimeout(300);
  }
}

export async function connectVirtualStandardGamepad(page, connected = true) {
  for (const frame of page.frames()) {
    await frame.evaluate((value) => globalThis.__retromTestGamepad?.connected(value), connected);
  }
  await page.waitForTimeout(500);
}
