import assert from "node:assert/strict";
import test from "node:test";
import { EmulatorBridge } from "../src/emulator-bridge.js";

function fakeFrameWindow() {
  let frame = 100;
  let playing = true;
  const frameWindow = {
    __RETROM_POST_MAIN_LOOP__: null,
    document: {
      body: { appendChild() {} },
      documentElement: { dataset: {}, removeAttribute() {} },
      querySelector() { return null; }
    }
  };
  const manager = {
    functions: { simulateInput() {} },
    getFrameNum: () => frame,
    getState: () => new Uint8Array(new Uint32Array([frame]).buffer),
    loadStateAndWait: async (state) => {
      frame = new Uint32Array(new Uint8Array(state).buffer.slice(0))[0];
      return { byteExact: true, changed: true, nativeCompletion: true, stateBytes: state.byteLength };
    },
    runNetplayFrame: async () => {
      frame += 1;
      frameWindow.__RETROM_POST_MAIN_LOOP__?.();
      playing = false;
      return frame;
    },
    simulateInput() {},
    toggleFastForward() {},
    toggleMainLoop(value) { playing = value === 1; }
  };
  frameWindow.EJS_emulator = {
    gameManager: manager,
    muted: false,
    setVolume() {},
    volume: 0.5
  };
  return { frameWindow, get playing() { return playing; } };
}

test("state load resets the native frame observer after a rewind", async () => {
  const fake = fakeFrameWindow();
  const bridge = new EmulatorBridge(fake.frameWindow, "test");
  let observed = 0;
  bridge.installFrameHook(() => { observed += 1; });

  await bridge.runExactFrame();
  assert.equal(observed, 1);

  const rewound = new Uint8Array(new Uint32Array([90]).buffer);
  const result = await bridge.loadStateAndWait(rewound);
  assert.equal(result.nativeCompletion, true);
  assert.equal(bridge.getFrame(), 90);

  await bridge.runExactFrame();
  assert.equal(bridge.getFrame(), 91);
  assert.equal(observed, 2, "the first post-load frame must not be suppressed by the old counter");
  assert.equal(fake.playing, false);
});
