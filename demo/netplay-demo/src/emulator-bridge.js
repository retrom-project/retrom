import { CONTROL_COUNT, PLAYER_COUNT } from "./protocol.js";

function wait(delayMs) {
  return new Promise((resolve) => setTimeout(resolve, delayMs));
}

function equalBytes(left, right) {
  if (left.byteLength !== right.byteLength) return false;
  for (let index = 0; index < left.byteLength; index += 1) {
    if (left[index] !== right[index]) return false;
  }
  return true;
}

export class EmulatorBridge {
  #frameWindow;
  #lastApplied = Array.from({ length: PLAYER_COUNT }, () => Array.from({ length: CONTROL_COUNT }, () => 0));
  #playing = true;
  #side;

  constructor(frameWindow, side) {
    if (!frameWindow) throw new TypeError("iframe contentWindow is required");
    this.#frameWindow = frameWindow;
    this.#side = side;
  }

  static async connect(iframe, side, timeoutMs = 120000) {
    const deadline = performance.now() + timeoutMs;
    while (performance.now() < deadline) {
      const player = iframe.contentWindow?.__NETPLAY_PLAYER__;
      if (player?.phase === "error") {
        throw new Error(`${side} player failed: ${player.errors.join("; ")}`);
      }
      if (player?.phase === "started" && iframe.contentWindow?.EJS_emulator?.gameManager) {
        return new EmulatorBridge(iframe.contentWindow, side);
      }
      await wait(50);
    }
    throw new Error(`${side} EmulatorJS startup timed out`);
  }

  get profile() {
    return structuredClone(this.#frameWindow.__NETPLAY_PLAYER__.profile);
  }

  get capabilities() {
    return structuredClone(this.#frameWindow.__NETPLAY_PLAYER__.capabilities);
  }

  getFrame() {
    return this.#manager().getFrameNum();
  }

  async waitForFrames(minimumDelta = 120, timeoutMs = 30000) {
    const start = this.getFrame();
    const deadline = performance.now() + timeoutMs;
    while (performance.now() < deadline) {
      if (this.getFrame() - start >= minimumDelta) return this.getFrame();
      await wait(16);
    }
    throw new Error(`${this.#side} frames did not advance by ${minimumDelta}`);
  }

  pauseAtFrame(targetFrame, timeoutMs = 30000) {
    if (!Number.isSafeInteger(targetFrame) || targetFrame <= this.getFrame()) {
      throw new Error(`${this.#side} pause target must be in the future`);
    }
    const original = this.#frameWindow.__RETROM_POST_MAIN_LOOP__;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        if (this.#frameWindow.__RETROM_POST_MAIN_LOOP__ === wrapper) {
          this.#frameWindow.__RETROM_POST_MAIN_LOOP__ = original;
        }
        reject(new Error(`${this.#side} did not reach alignment frame ${targetFrame}`));
      }, timeoutMs);
      const wrapper = () => {
        if (original) original();
        const frame = this.getFrame();
        if (frame < targetFrame) return;
        clearTimeout(timer);
        this.pause();
        if (this.#frameWindow.__RETROM_POST_MAIN_LOOP__ === wrapper) {
          this.#frameWindow.__RETROM_POST_MAIN_LOOP__ = original;
        }
        if (frame === targetFrame) resolve(frame);
        else reject(new Error(`${this.#side} skipped alignment frame ${targetFrame}`));
      };
      this.#frameWindow.__RETROM_POST_MAIN_LOOP__ = wrapper;
    });
  }

  captureState() {
    return new Uint8Array(this.#manager().getState());
  }

  loadState(state) {
    this.#manager().loadState(new Uint8Array(state));
  }

  async loadStateAndWait(state, timeoutMs = 5000) {
    const expected = new Uint8Array(state);
    this.loadState(expected);
    const deadline = performance.now() + timeoutMs;
    let consecutiveMatches = 0;
    // EmulatorJS 4.2.3 queues load_state work and returns immediately. Let at
    // least one paused main-loop tick drain that queue before observing it.
    await wait(34);
    while (performance.now() < deadline) {
      consecutiveMatches = equalBytes(this.captureState(), expected)
        ? consecutiveMatches + 1
        : 0;
      if (consecutiveMatches >= 2) return;
      await wait(17);
    }
    throw new Error(`${this.#side} savestate load did not settle`);
  }

  pause() {
    if (!this.#playing) return;
    this.#manager().toggleMainLoop(0);
    this.#playing = false;
  }

  resume() {
    if (this.#playing) return;
    this.#manager().toggleMainLoop(1);
    this.#playing = true;
  }

  resetInputs() {
    const rawInput = this.#rawInput();
    for (let player = 0; player < PLAYER_COUNT; player += 1) {
      for (let control = 0; control < CONTROL_COUNT; control += 1) {
        rawInput(player, control, 0);
        this.#lastApplied[player][control] = 0;
      }
    }
  }

  applyInputs(players) {
    const rawInput = this.#rawInput();
    for (let player = 0; player < PLAYER_COUNT; player += 1) {
      for (let control = 0; control < CONTROL_COUNT; control += 1) {
        const value = players[player][control];
        if (value === this.#lastApplied[player][control]) continue;
        rawInput(player, control, value);
        this.#lastApplied[player][control] = value;
      }
    }
  }

  simulateLocalInput(control, value) {
    this.#manager().simulateInput(0, control, value);
  }

  installInputCapture(onInput) {
    const manager = this.#manager();
    const original = manager.simulateInput;
    if (typeof original !== "function") throw new Error(`${this.#side} public input hook is unavailable`);
    const wrapper = function captureLocalPlayer(player, control, value) {
      if (player !== 0 || control < 0 || control >= CONTROL_COUNT) return;
      onInput(control, value);
    };
    manager.simulateInput = wrapper;
    return () => {
      if (manager.simulateInput === wrapper) manager.simulateInput = original;
    };
  }

  installFrameHook(onFrameEnd) {
    const original = this.#frameWindow.__RETROM_POST_MAIN_LOOP__;
    const wrapper = function retromNetplayPostMainLoop() {
      if (original) original();
      onFrameEnd(thisBridge.getFrame());
    };
    const thisBridge = this;
    this.#frameWindow.__RETROM_POST_MAIN_LOOP__ = wrapper;
    return () => {
      if (this.#frameWindow.__RETROM_POST_MAIN_LOOP__ === wrapper) {
        this.#frameWindow.__RETROM_POST_MAIN_LOOP__ = original;
      }
    };
  }

  #emulator() {
    const emulator = this.#frameWindow.EJS_emulator;
    if (!emulator) throw new Error(`${this.#side} EmulatorJS instance is unavailable`);
    return emulator;
  }

  #manager() {
    const manager = this.#emulator().gameManager;
    if (!manager) throw new Error(`${this.#side} GameManager is unavailable`);
    return manager;
  }

  #rawInput() {
    const input = this.#manager().functions?.simulateInput;
    if (typeof input !== "function") throw new Error(`${this.#side} raw input injection is unavailable`);
    return input;
  }
}
