import { CONTROL_COUNT, PLAYER_COUNT } from "./protocol.js";

function wait(delayMs) {
  return new Promise((resolve) => setTimeout(resolve, delayMs));
}

export class EmulatorBridge {
  #frameWindow;
  #internalStepping = false;
  #lastApplied = Array.from({ length: PLAYER_COUNT }, () => Array.from({ length: CONTROL_COUNT }, () => 0));
  #lastObservedFrame = 0;
  #playing = true;
  #replay = null;
  #replayMetrics = { runs: 0, frames: 0, mutedRuns: 0, totalMs: 0 };
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
        if (frame === targetFrame) {
          void this.waitForPause(frame).then(() => resolve(frame), reject);
        }
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
    const load = this.#manager().loadStateAndWait;
    if (typeof load !== "function") throw new Error(`${this.#side} waitable state-load patch is unavailable`);
    await this.waitForPause();
    const ownsSuppression = this.#replay === null;
    if (ownsSuppression) this.beginReplay();
    this.#internalStepping = true;
    try {
      const result = await load.call(this.#manager(), new Uint8Array(state), timeoutMs);
      // A savestate may rewind the native RetroArch frame counter. Reset the
      // observer epoch as part of the same completion boundary; otherwise the
      // frame hook suppresses post-load frames until the old counter is passed.
      this.#lastObservedFrame = this.getFrame();
      return result;
    } finally {
      this.#internalStepping = false;
      if (ownsSuppression) this.endReplay(0);
    }
  }

  async runExactFrame({ suppressHook = false } = {}) {
    const run = this.#manager().runNetplayFrame;
    if (typeof run !== "function") throw new Error(`${this.#side} exact frame-step patch is unavailable`);
    const previousStepping = this.#internalStepping;
    if (suppressHook) this.#internalStepping = true;
    try {
      return await run.call(this.#manager());
    } finally {
      this.#internalStepping = previousStepping;
    }
  }

  async waitForPause(expectedFrame = this.getFrame(), timeoutMs = 1000) {
    const deadline = performance.now() + timeoutMs;
    let previous = expectedFrame;
    let stableSamples = 0;
    while (performance.now() < deadline) {
      await wait(12);
      const current = this.getFrame();
      if (current === previous) stableSamples += 1;
      else {
        previous = current;
        stableSamples = 0;
        this.#manager().toggleMainLoop(0);
      }
      if (stableSamples >= 3) return current;
    }
    throw new Error(`${this.#side} main loop did not settle in the paused state`);
  }

  beginReplay() {
    if (this.#replay) throw new Error(`${this.#side} replay is already active`);
    this.pause();
    const emulator = this.#emulator();
    const startedAt = performance.now();
    const volume = Number.isFinite(emulator.volume) ? emulator.volume : 0;
    const muted = emulator.muted === true;
    const sourceCanvas = this.#frameWindow.document.querySelector("#game canvas");
    let overlay = null;
    if (sourceCanvas) {
      overlay = this.#frameWindow.document.createElement("canvas");
      overlay.className = "netplay-replay-frame";
      overlay.width = sourceCanvas.width;
      overlay.height = sourceCanvas.height;
      try {
        overlay.getContext("2d").drawImage(sourceCanvas, 0, 0);
        this.#frameWindow.document.body.appendChild(overlay);
      } catch {
        overlay = null;
      }
    }
    if (typeof emulator.setVolume === "function") emulator.setVolume(0);
    this.#manager().toggleFastForward(1);
    this.#frameWindow.document.documentElement.dataset.netplayReplay = "true";
    this.#replay = { muted, overlay, startedAt, volume };
    this.#replayMetrics.runs += 1;
    this.#replayMetrics.mutedRuns += 1;
  }

  endReplay(frames) {
    if (!this.#replay) return;
    const emulator = this.#emulator();
    this.#manager().toggleFastForward(0);
    this.#frameWindow.document.documentElement.removeAttribute("data-netplay-replay");
    this.#replay.overlay?.remove();
    emulator.muted = this.#replay.muted;
    if (typeof emulator.setVolume === "function") {
      emulator.setVolume(this.#replay.muted ? 0 : this.#replay.volume);
    }
    this.#replayMetrics.frames += frames;
    this.#replayMetrics.totalMs += performance.now() - this.#replay.startedAt;
    this.#replay = null;
  }

  getReplayMetrics() {
    return { ...this.#replayMetrics, active: this.#replay !== null };
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

  applyInputs(players, { force = false } = {}) {
    const rawInput = this.#rawInput();
    for (let player = 0; player < PLAYER_COUNT; player += 1) {
      for (let control = 0; control < CONTROL_COUNT; control += 1) {
        const value = players[player][control];
        if (!force && value === this.#lastApplied[player][control]) continue;
        rawInput(player, control, value);
        this.#lastApplied[player][control] = value;
      }
    }
  }

  injectUntrackedInput(player, control, value) {
    this.#rawInput()(player, control, value);
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
    this.#lastObservedFrame = this.getFrame();
    const wrapper = function retromNetplayPostMainLoop() {
      if (original) original();
      const frame = thisBridge.getFrame();
      if (frame <= thisBridge.#lastObservedFrame) return;
      thisBridge.#lastObservedFrame = frame;
      if (!thisBridge.#internalStepping) onFrameEnd(frame);
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
