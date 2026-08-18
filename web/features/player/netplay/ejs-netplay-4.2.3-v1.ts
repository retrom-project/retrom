import { sha256 } from "@/lib/crypto";
import type { EmulatorInstance } from "../adapters/ejs-4.2.3-v2";
import type { CanonicalInput } from "./rollback";

export const netplayAdapterID = "ejs-netplay-4.2.3-v1" as const;
export const netplayRuntimeVersion = "4.2.3" as const;

const VERSION_URL = "https://cdn.emulatorjs.org/stable/data/version.json";

type RuntimeModuleConfig = {
  postMainLoop?: () => void;
  print?: (...args: unknown[]) => void;
  printErr?: (...args: unknown[]) => void;
  [name: string]: unknown;
};
type RuntimeFactory = ((config: RuntimeModuleConfig) => unknown) & { retromNetplayFrameHook?: boolean };
type GameManagerPrototype = {
  mountFileSystems?: () => Promise<void>;
  loadStateAndWait?: (state: Uint8Array, timeoutMs?: number) => Promise<{ byteExact: boolean }>;
  runNetplayFrame?: (timeoutMs?: number) => Promise<number>;
};
type GameManagerConstructor = { prototype?: GameManagerPrototype };
type NetplayPatchWindow = Window & {
  console: Console;
  EJS_Runtime?: RuntimeFactory;
  EJS_GameManager?: GameManagerConstructor;
  __RETROM_POST_MAIN_LOOP__?: () => void;
};
type LoadSignal = { reject: (error: Error) => void; resolve: () => void };
type RuntimeFileSystem = {
  unlink?: (path: string) => void;
  writeFile?: (path: string, bytes: Uint8Array) => void;
};
type NetplayManager = Required<Pick<NonNullable<EmulatorInstance["gameManager"]>,
  "getState" | "getFrameNum" | "simulateInput" | "toggleMainLoop" | "loadStateAndWait" | "runNetplayFrame">> & {
    functions: {
      simulateInput: (player: number, control: number, value: number) => void;
      loadState?: (...args: unknown[]) => unknown;
    };
    toggleFastForward?: (running: boolean) => void;
  };

function equalBytes(left: Uint8Array, right: Uint8Array) {
  if (left.byteLength !== right.byteLength) return false;
  for (let index = 0; index < left.byteLength; index += 1) if (left[index] !== right[index]) return false;
  return true;
}

/** Install the v4.2.3 hooks before loader.js assigns either constructor. */
export function installEmulatorJs423NetplayCompatibility(playerWindow: Window = window) {
  const target = playerWindow as NetplayPatchWindow;
  const loadSignals: LoadSignal[] = [];
  const prototypeRestores: Array<() => void> = [];
  let active = true;

  const registerLoadSignal = () => {
    let signal!: LoadSignal;
    const promise = new Promise<void>((resolve, reject) => { signal = { resolve, reject }; });
    loadSignals.push(signal);
    return {
      promise,
      complete: () => {
        const index = loadSignals.indexOf(signal);
        if (index < 0) return;
        loadSignals.splice(index, 1);
        signal.resolve();
      },
      cancel: () => {
        const index = loadSignals.indexOf(signal);
        if (index >= 0) loadSignals.splice(index, 1);
      },
    };
  };
  const observeNativeLog = (args: unknown[]) => {
    const message = args.map(String).join(" ");
    if (!message.includes("[State]") || !message.includes("game.state")) return;
    const signal = loadSignals.shift();
    if (!signal) return;
    if (/failed/i.test(message)) target.queueMicrotask(() => signal.reject(new Error("STATE_INVALID")));
    else if (/loading state/i.test(message)) target.queueMicrotask(signal.resolve);
    else loadSignals.unshift(signal);
  };
  const patchManager = (constructor: GameManagerConstructor | undefined) => {
    const prototype = constructor?.prototype;
    if (!prototype || typeof prototype.mountFileSystems !== "function") throw new Error("NETPLAY_RUNTIME_COMPATIBILITY_UNAVAILABLE");
    if (prototype.loadStateAndWait || prototype.runNetplayFrame) throw new Error("NETPLAY_RUNTIME_COMPATIBILITY_UNAVAILABLE");
    const originalMount = prototype.mountFileSystems;
    const mountInMemory = async function (this: { mkdir?: (path: string) => void }) {
      if (typeof this.mkdir !== "function") throw new Error("NETPLAY_RUNTIME_COMPATIBILITY_UNAVAILABLE");
      this.mkdir("/data");
      this.mkdir("/data/saves");
    };
    const loadStateAndWait = async function (this: NonNullable<EmulatorInstance["gameManager"]>, state: Uint8Array, timeoutMs = 5_000) {
      const fileSystem = this.FS as RuntimeFileSystem | undefined;
      const functions = (this as { functions?: { loadState?: (...args: unknown[]) => unknown } }).functions;
      if (!this.getState || !this.toggleMainLoop || !fileSystem?.writeFile || !fileSystem.unlink ||
        typeof functions?.loadState !== "function") throw new Error("NETPLAY_RUNTIME_COMPATIBILITY_UNAVAILABLE");
      const expected = new Uint8Array(state);
      const completion = registerLoadSignal();
      let timer: number | undefined;
      try {
        try { fileSystem.unlink("game.state"); } catch { /* absent before the first load */ }
        fileSystem.writeFile("/game.state", expected);
        (this as { clearEJSResetTimer?: () => void }).clearEJSResetTimer?.();
        functions.loadState("game.state", 0);
        this.toggleMainLoop(true);
        await Promise.race([
          completion.promise,
          new Promise<never>((_, reject) => {
            timer = target.setTimeout(() => reject(new Error("STATE_LOAD_TIMEOUT")), timeoutMs);
          }),
        ]);
        this.toggleMainLoop(false);
        const byteExact = equalBytes(new Uint8Array(this.getState()), expected);
        return { byteExact };
      } finally {
        if (timer !== undefined) target.clearTimeout(timer);
        this.toggleMainLoop(false);
        completion.cancel();
        try { fileSystem.unlink("game.state"); } catch { /* native code may already remove it */ }
      }
    };
    const runNetplayFrame = function (this: NonNullable<EmulatorInstance["gameManager"]>, timeoutMs = 1_000) {
      if (!this.getFrameNum || !this.toggleMainLoop) return Promise.reject(new Error("NETPLAY_RUNTIME_COMPATIBILITY_UNAVAILABLE"));
      return new Promise<number>((resolve, reject) => {
        const original = target.__RETROM_POST_MAIN_LOOP__;
        const startFrame = this.getFrameNum!();
        const wrapper = () => {
          original?.();
          const completedFrame = this.getFrameNum!();
          if (completedFrame <= startFrame) return;
          target.clearTimeout(timer);
          this.toggleMainLoop!(false);
          if (target.__RETROM_POST_MAIN_LOOP__ === wrapper) target.__RETROM_POST_MAIN_LOOP__ = original;
          resolve(completedFrame);
        };
        const timer = target.setTimeout(() => {
          this.toggleMainLoop!(false);
          if (target.__RETROM_POST_MAIN_LOOP__ === wrapper) target.__RETROM_POST_MAIN_LOOP__ = original;
          reject(new Error("NETPLAY_FRAME_STEP_TIMEOUT"));
        }, timeoutMs);
        target.__RETROM_POST_MAIN_LOOP__ = wrapper;
        this.toggleMainLoop!(true);
      });
    };
    prototype.mountFileSystems = mountInMemory;
    prototype.loadStateAndWait = loadStateAndWait;
    prototype.runNetplayFrame = runNetplayFrame;
    prototypeRestores.push(() => {
      prototype.mountFileSystems = originalMount;
      Reflect.deleteProperty(prototype, "loadStateAndWait");
      Reflect.deleteProperty(prototype, "runNetplayFrame");
    });
  };

  const managerDescriptor = Object.getOwnPropertyDescriptor(target, "EJS_GameManager");
  if (managerDescriptor && !managerDescriptor.configurable) throw new Error("NETPLAY_RUNTIME_COMPATIBILITY_UNAVAILABLE");
  let managerConstructor = target.EJS_GameManager;
  if (managerConstructor) patchManager(managerConstructor);
  Object.defineProperty(target, "EJS_GameManager", {
    configurable: true,
    enumerable: managerDescriptor?.enumerable ?? true,
    get: () => managerConstructor,
    set: (constructor: GameManagerConstructor | undefined) => {
      patchManager(constructor);
      managerConstructor = constructor;
    },
  });

  const wrapRuntime = (factory: RuntimeFactory | undefined) => {
    if (typeof factory !== "function") throw new Error("NETPLAY_RUNTIME_COMPATIBILITY_UNAVAILABLE");
    if (factory.retromNetplayFrameHook) return factory;
    const wrapped = function (this: unknown, moduleConfig: RuntimeModuleConfig) {
      const originalPostMainLoop = moduleConfig?.postMainLoop;
      const patchedConfig: RuntimeModuleConfig = {
        ...moduleConfig,
        print: (...args: unknown[]) => { moduleConfig?.print?.(...args); observeNativeLog(args); },
        printErr: (...args: unknown[]) => { moduleConfig?.printErr?.(...args); observeNativeLog(args); },
        postMainLoop: () => { originalPostMainLoop?.(); target.__RETROM_POST_MAIN_LOOP__?.(); },
      };
      const originalLog = target.console.log;
      const originalError = target.console.error;
      target.console.log = function observeStateLoad(...args: unknown[]) {
        originalLog.apply(this, args);
        observeNativeLog(args);
      };
      target.console.error = function observeStateLoadError(...args: unknown[]) {
        originalError.apply(this, args);
        observeNativeLog(args);
      };
      try {
        return Reflect.apply(factory, this, [patchedConfig]);
      } finally {
        target.console.log = originalLog;
        target.console.error = originalError;
      }
    } as RuntimeFactory;
    Object.defineProperty(wrapped, "retromNetplayFrameHook", { value: true });
    return wrapped;
  };
  const runtimeDescriptor = Object.getOwnPropertyDescriptor(target, "EJS_Runtime");
  if (runtimeDescriptor && !runtimeDescriptor.configurable) throw new Error("NETPLAY_RUNTIME_COMPATIBILITY_UNAVAILABLE");
  let runtimeFactory = target.EJS_Runtime ? wrapRuntime(target.EJS_Runtime) : undefined;
  Object.defineProperty(target, "EJS_Runtime", {
    configurable: true,
    enumerable: runtimeDescriptor?.enumerable ?? true,
    get: () => runtimeFactory,
    set: (factory: RuntimeFactory | undefined) => { runtimeFactory = wrapRuntime(factory); },
  });

  const originalFetch = target.fetch.bind(target);
  target.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" || input instanceof URL ? String(input) : input.url;
    if (url === VERSION_URL) return Promise.resolve(new Response(JSON.stringify({ version: "4.2.3", current_version: "4.2.3" }), {
      status: 200, headers: { "Content-Type": "application/json" },
    }));
    return originalFetch(input, init);
  }) as typeof fetch;

  return () => {
    if (!active) return;
    active = false;
    target.fetch = originalFetch;
    for (const restore of prototypeRestores.reverse()) restore();
    if (managerDescriptor) Object.defineProperty(target, "EJS_GameManager", managerDescriptor);
    else Reflect.deleteProperty(target, "EJS_GameManager");
    if (runtimeDescriptor) Object.defineProperty(target, "EJS_Runtime", runtimeDescriptor);
    else Reflect.deleteProperty(target, "EJS_Runtime");
    Reflect.deleteProperty(target, "__RETROM_POST_MAIN_LOOP__");
    for (const signal of loadSignals.splice(0)) signal.reject(new Error("NETPLAY_SESSION_ENDED"));
  };
}

export class EJSNetplayFrameBridge {
  private readonly manager: NetplayManager;
  private readonly nativeSimulateInput: (player: number, control: number, value: number) => void;
  private readonly publicSimulateInput: (player: number, control: number, value: number) => void;
  private readonly inputCapture: (player: number, control: number, value: number) => void;
  private readonly localControls = Array<number>(24).fill(0);
  private closed = false;

  constructor(private readonly runtime: EmulatorInstance) {
    const manager = runtime.gameManager;
    const rawInput = manager?.functions?.simulateInput;
    if (!manager?.getState || !manager.getFrameNum || !manager.simulateInput || !manager.toggleMainLoop ||
      !manager.loadStateAndWait || !manager.runNetplayFrame || !rawInput) {
      throw new Error("NETPLAY_RUNTIME_COMPATIBILITY_UNAVAILABLE");
    }
    this.manager = manager as NetplayManager;
    this.publicSimulateInput = manager.simulateInput;
    this.nativeSimulateInput = rawInput.bind(manager.functions);
    this.inputCapture = (player, control, value) => {
      if (player === 0 && Number.isInteger(control) && control >= 0 && control < 24 && Number.isFinite(value)) {
        this.localControls[control] = Math.max(-32768, Math.min(32767, Math.trunc(value)));
      }
    };
    manager.simulateInput = this.inputCapture;
  }

  async pauseAtBoundary() {
    if (this.closed) throw new Error("NETPLAY_SESSION_ENDED");
    await this.manager.runNetplayFrame();
    this.runtime.paused = true;
  }

  captureState() {
    const value = this.manager.getState();
    if (!ArrayBuffer.isView(value) || value.byteLength < 8) throw new Error("STATE_INVALID");
    return new Uint8Array(value);
  }

  async loadStateAndWait(bytes: Uint8Array) {
    const result = await this.loadStateForTransfer(bytes);
    if (!result.coreExact) throw new Error("STATE_INVALID");
  }

  async loadStateForTransfer(bytes: Uint8Array) {
    if (!bytes.byteLength) throw new Error("STATE_INVALID");
    const expectedCore = coreStateBytes(bytes);
    const recaptured = await this.withSuppressedOutput(async () => {
      await this.manager.loadStateAndWait(new Uint8Array(bytes));
      return this.captureState();
    });
    this.runtime.paused = true;
    const recapturedCore = coreStateBytes(recaptured);
    let firstCoreMismatch = -1;
    const comparedLength = Math.min(expectedCore.byteLength, recapturedCore.byteLength);
    for (let index = 0; index < comparedLength; index += 1) {
      if (expectedCore[index] !== recapturedCore[index]) { firstCoreMismatch = index; break; }
    }
    if (firstCoreMismatch < 0 && expectedCore.byteLength !== recapturedCore.byteLength) firstCoreMismatch = comparedLength;
    return {
      recaptured,
      byteExact: equalBytes(recaptured, bytes),
      coreExact: firstCoreMismatch < 0,
      expectedCoreBytes: expectedCore.byteLength,
      recapturedCoreBytes: recapturedCore.byteLength,
      firstCoreMismatch,
    };
  }

  async runNetplayFrame(input: CanonicalInput, suppressOutput = false) {
    if (input.length !== 4 || input.some((controls) => controls.length !== 24)) throw new Error("NETPLAY_INPUT_INVALID");
    const run = async () => {
      for (let player = 0; player < 4; player += 1) {
        for (let control = 0; control < 24; control += 1) this.nativeSimulateInput(player, control, input[player]![control]!);
      }
      await this.manager.runNetplayFrame();
      this.runtime.paused = true;
    };
    if (suppressOutput) await this.withSuppressedOutput(run);
    else await run();
  }

  private async withSuppressedOutput<T>(work: () => Promise<T>) {
    const canvasVisibility = this.runtime.canvas?.style.visibility;
    const muted = this.runtime.muted === true;
    const volume = Number.isFinite(this.runtime.volume) ? this.runtime.volume! : 1;
    try {
      if (this.runtime.canvas) this.runtime.canvas.style.visibility = "hidden";
      this.runtime.setVolume?.(0);
      this.runtime.muted = true;
      this.manager.toggleFastForward?.(true);
      return await work();
    } finally {
      this.manager.toggleFastForward?.(false);
      if (this.runtime.canvas) this.runtime.canvas.style.visibility = canvasVisibility ?? "";
      this.runtime.muted = muted;
      this.runtime.setVolume?.(muted ? 0 : volume);
    }
  }

  close() {
    this.closed = true;
    this.manager.toggleMainLoop(false);
    if (this.manager.simulateInput === this.inputCapture) this.manager.simulateInput = this.publicSimulateInput;
    this.localControls.fill(0);
  }

  sampleLocalControls() { return [...this.localControls]; }
  resetLocalControls() { this.localControls.fill(0); }
	setLocalControlForTest(control: number, value: number) {
		if (process.env.NODE_ENV === "production") return;
		if (!Number.isInteger(control) || control < 0 || control >= this.localControls.length || !Number.isFinite(value)) {
			throw new Error("NETPLAY_INPUT_INVALID");
		}
		this.localControls[control] = Math.max(-32768, Math.min(32767, Math.trunc(value)));
	}
}

export function coreStateBytes(value: Uint8Array) {
  if (new TextDecoder().decode(value.subarray(0, 7)) !== "RASTATE" || value[7] !== 1) throw new Error("STATE_INVALID");
  const view = new DataView(value.buffer, value.byteOffset, value.byteLength);
  for (let offset = 8; offset + 8 <= value.byteLength;) {
    const marker = new TextDecoder().decode(value.subarray(offset, offset + 4));
    const size = view.getUint32(offset + 4, true); const start = offset + 8; const end = start + size;
    if (end > value.byteLength) throw new Error("STATE_INVALID");
    if (marker === "MEM ") return value.subarray(start, end);
    if (marker === "END ") break;
    offset = start + ((size + 7) & ~7);
  }
  throw new Error("STATE_INVALID");
}

// FBNeo scans the Neo Geo AY8910 sound generator through its driver state (96 bytes), then
// nYM2610Position and nAY8910Position (8 bytes), immediately before uPD4990A.
// These audio-output states depend on browser sample scheduling, not game logic.
// Its libretro serializer also writes nCurrentFrame (4 bytes), followed by the
// 1588 bytes registered by YM2610_save_state, before NeoScan writes game state.
const fbneoLibretroFrameCounterBytes = 4;
const fbneoNeoGeoYM2610StateBytes = 1_588;
const fbneoNeoGeoAY8910StateBytes = 96;
const fbneoYM2610OutputPositionBytes = 8;
const fbneoNeoGeoRTCTicksPerSecond = 12_000_000;

export function checkpointCoreStateBytes(value: Uint8Array, profileID: string) {
  if (profileID !== "fbneo-423-v1") return value;
  const view = new DataView(value.buffer, value.byteOffset, value.byteLength);
  const candidates: number[] = [];
  for (let offset = 0; offset + 68 <= value.byteLength; offset += 4) {
    const seconds = view.getUint32(offset, true);
    const minutes = view.getUint32(offset + 4, true);
    const hours = view.getUint32(offset + 8, true);
    const day = view.getUint32(offset + 12, true);
    const month = view.getUint32(offset + 16, true);
    const year = view.getUint32(offset + 20, true);
    const weekDay = view.getUint32(offset + 24, true);
    const mode = view.getInt32(offset + 28, true);
    const tpMode = view.getInt32(offset + 32, true);
    const command = view.getUint32(offset + 44, true);
    const outputs = view.getUint32(offset + 60, true);
    const ticksPerSecond = view.getUint32(offset + 64, true);
    if (seconds < 60 && minutes < 60 && hours < 24 && day >= 1 && day <= 31 && month >= 1 && month <= 12 &&
      year < 100 && weekDay < 7 && mode >= 0 && mode <= 2 && tpMode >= 0 && tpMode <= 2 && command <= 15 &&
      (outputs & 0xff) <= 1 && ((outputs >>> 8) & 0xff) <= 1 && ((outputs >>> 16) & 0xff) <= 1 &&
      ticksPerSecond === fbneoNeoGeoRTCTicksPerSecond) candidates.push(offset);
  }
  if (candidates.length !== 1) return value;
  const audioEnd = candidates[0]!;
  const audioStart = audioEnd - fbneoNeoGeoAY8910StateBytes - fbneoYM2610OutputPositionBytes;
  const driverStateStart = fbneoLibretroFrameCounterBytes + fbneoNeoGeoYM2610StateBytes;
  if (audioStart < 0 || driverStateStart > value.byteLength) return value;
  const projected = new Uint8Array(value);
  projected.fill(0, fbneoLibretroFrameCounterBytes, driverStateStart);
  projected.fill(0, audioStart, audioEnd);
  return projected;
}

export async function digestHex(value: Uint8Array) {
  const digest = await sha256(value);
  return [...new Uint8Array(digest)].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}
