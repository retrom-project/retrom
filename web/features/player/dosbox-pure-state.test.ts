import { describe, expect, it, vi } from "vitest";
import {
  canCreateRecoverableManualState,
  installDOSBoxPureStateCompatibility,
  patchDOSBoxPureStateStack,
  readDOSBoxPureState,
  requiresDOSBoxPureStateCompatibility,
} from "./dosbox-pure-state";

const marker = [
  0x23, 0x0c, 0x45, 0x04, 0x40, 0x20, 0x01, 0x41, 0xc0, 0x02,
  0x6a, 0x24, 0x00, 0x20, 0x01, 0x41, 0x10, 0x6a, 0x0f,
];
const linkedStackHigh = [0xf0, 0xec, 0x80, 0x0e];
const compatibleStackHigh = [0xf0, 0xec, 0x80, 0x2c];

function wasmWithCustomPayload(payload: number[]) {
  const customSection = [1, 0x78, ...payload];
  return Uint8Array.of(0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x00, customSection.length, ...customSection);
}

function countPattern(bytes: Uint8Array, pattern: number[]) {
  let count = 0;
  for (let index = 0; index <= bytes.byteLength - pattern.length; index += 1) {
    if (pattern.every((value, offset) => bytes[index + offset] === value)) {count += 1;}
  }
  return count;
}

function rastate(payload: number[], wrapper = 0) {
  const padding = (8 - payload.length % 8) % 8;
  return Uint8Array.of(
    0x52, 0x41, 0x53, 0x54, 0x41, 0x54, 0x45, 0x01,
    0x4d, 0x45, 0x4d, 0x20, payload.length, 0, 0, 0,
    ...payload, ...Array<number>(padding).fill(0),
    0x41, 0x43, 0x48, 0x56, 1, 0, 0, 0, wrapper, ...Array<number>(7).fill(0),
    0x45, 0x4e, 0x44, 0x20, 0, 0, 0, 0,
  );
}

describe("DOSBox Pure explicit state compatibility", () => {
  it("patches only the two linked stack limits in the pinned valid WASM shape", () => {
    const source = wasmWithCustomPayload([...linkedStackHigh, ...marker, ...linkedStackHigh]);
    expect(WebAssembly.validate(source)).toBe(true);
    const patched = patchDOSBoxPureStateStack(source);
    expect(patched).not.toBeNull();
    expect(WebAssembly.validate(patched!)).toBe(true);
    expect(countPattern(patched!, linkedStackHigh)).toBe(0);
    expect(countPattern(patched!, compatibleStackHigh)).toBe(2);
    expect(source).not.toEqual(patched);
  });

  it("ignores unrelated WASM and rejects a drifted matching artifact", () => {
    expect(patchDOSBoxPureStateStack(wasmWithCustomPayload([1, 2, 3]))).toBeNull();
    expect(() => patchDOSBoxPureStateStack(wasmWithCustomPayload([...marker, ...linkedStackHigh])))
      .toThrow("PLAYER_DOS_STATE_COMPATIBILITY_UNAVAILABLE");
  });

  it("copies the state and frees only its heap allocation, never the stack description", () => {
    const heap = new Uint8Array(256);
    heap.set([7, 8, 9], 64);
    const free = vi.fn();
    const state = readDOSBoxPureState({
      HEAPU8: heap,
      _save_state_info: () => 24,
      UTF8ToString: (pointer) => pointer === 24 ? "3|64|1" : "",
      _free: free,
    });
    expect(state).toEqual(Uint8Array.of(7, 8, 9));
    expect(free).toHaveBeenCalledExactlyOnceWith(64);
    heap[64] = 1;
    expect(state[0]).toBe(7);
  });

  it("rejects unavailable and out-of-bounds state descriptions", () => {
    const free = vi.fn();
    expect(() => readDOSBoxPureState({
      HEAPU8: new Uint8Array(32), _save_state_info: () => 4,
      UTF8ToString: () => "Error writing data||0", _free: free,
    })).toThrow("PLAYER_STATE_UNAVAILABLE");
    expect(() => readDOSBoxPureState({
      HEAPU8: new Uint8Array(32), _save_state_info: () => 4,
      UTF8ToString: () => "20|20|1", _free: free,
    })).toThrow("PLAYER_STATE_UNAVAILABLE");
    expect(free).not.toHaveBeenCalled();
  });

  it("is scoped to the pinned DOSBox runtime", () => {
    expect(requiresDOSBoxPureStateCompatibility({ emulatorjsVersion: "4.3.0-pre", runtimeCore: "dosbox_pure" })).toBe(true);
    expect(requiresDOSBoxPureStateCompatibility({ emulatorjsVersion: "4.2.3", runtimeCore: "dosbox_pure" })).toBe(false);
    expect(requiresDOSBoxPureStateCompatibility({ emulatorjsVersion: "4.3.0-pre", runtimeCore: "azahar" })).toBe(false);
  });

  it("allows recoverable saves only after a DOS launch locks a concrete program", () => {
    expect(canCreateRecoverableManualState({ runtimeCore: "dosbox_pure", dosEntry: "PCYR2/pre2.exe" })).toBe(true);
    expect(canCreateRecoverableManualState({ runtimeCore: "dosbox_pure", dosEntry: null })).toBe(false);
    expect(canCreateRecoverableManualState({ runtimeCore: "fbneo", dosEntry: null })).toBe(true);
  });

  it("restores after the program is serializable and native completion is observed", async () => {
    vi.useFakeTimers();
    const installation = installDOSBoxPureStateCompatibility(window);
    let runtimeConfig: { postMainLoop?: () => void; print?: (...args: unknown[]) => void } = {};
    const runtimeWindow = window as Window & { EJS_Runtime?: (config: typeof runtimeConfig) => unknown };
    runtimeWindow.EJS_Runtime = vi.fn((config) => {
      runtimeConfig = config;
      return {};
    });
    runtimeWindow.EJS_Runtime?.({
      postMainLoop: vi.fn(),
    });
    await window.WebAssembly.instantiate(wasmWithCustomPayload([...linkedStackHigh, ...marker, ...linkedStackHigh]));
    const heap = new Uint8Array(256);
    const initialState = rastate([7, 8, 9]);
    const restoredState = rastate([3, 2, 1], 9);
    heap.set(initialState, 64);
    const files = new Map<string, Uint8Array>();
    const loop = vi.fn();
    const nativeLoad = vi.fn(() => {
      heap.set(restoredState, 64);
      runtimeConfig.print?.(`[INFO] [State] Loading state \"/game.state\", ${restoredState.byteLength} bytes.`);
    });
    class Manager {
      Module: {
        HEAPU8: Uint8Array;
        _save_state_info: () => number;
        UTF8ToString: () => string;
        _free: () => void;
        postMainLoop?: () => void;
      } = {
        HEAPU8: heap,
        _save_state_info: () => 24,
        UTF8ToString: () => `${restoredState.byteLength}|64|1`,
        _free: () => undefined,
      };
      functions = { loadState: nativeLoad };
      FS = {
        analyzePath: (path: string) => ({ exists: files.has(path) }),
        mkdir: () => undefined,
        writeFile: (path: string, bytes: Uint8Array) => files.set(path, new Uint8Array(bytes)),
        unlink: (path: string) => {
          if (!files.delete(path)) {throw new Error("ENOENT");}
        },
      };
      getState() { return Uint8Array.of(0); }
      toggleMainLoop(running: boolean) {
        loop(running);
        if (running) {window.setTimeout(() => runtimeConfig.postMainLoop?.(), 0);}
      }
    }
    const manager = new Manager();
    installation.prepare({ on: () => undefined, gameManager: manager });
    const compatibleManager = manager as Manager & {
      loadExplicitStateAndWait: (state: Uint8Array) => Promise<void>;
    };
    const restore = compatibleManager.loadExplicitStateAndWait(restoredState);
    await vi.runAllTimersAsync();
    await expect(restore).resolves.toBeUndefined();
    expect(nativeLoad).toHaveBeenCalledExactlyOnceWith("/game.state", 0);
    expect(loop.mock.calls.at(-1)).toEqual([false]);
    expect(files.size).toBe(0);

    manager.functions.loadState = vi.fn(() => {
      runtimeConfig.print?.(`[INFO] [State] Loading state \"/game.state\", ${restoredState.byteLength} bytes.`);
      runtimeConfig.print?.("[ERROR] [State] Failed to load state \"/game.state\".");
    });
    const failedRestore = compatibleManager.loadExplicitStateAndWait(restoredState);
    const failedExpectation = expect(failedRestore).rejects.toThrow("PLAYER_SAVE_STATE_RESTORE_FAILED");
    await vi.runAllTimersAsync();
    await failedExpectation;
    expect(loop.mock.calls.at(-1)).toEqual([false]);
    expect(files.size).toBe(0);
    installation.cleanup();
    vi.useRealTimers();
  });

  it("fails closed when the selected state has no bounded RASTATE core block", async () => {
    vi.useFakeTimers();
    const installation = installDOSBoxPureStateCompatibility(window);
    await window.WebAssembly.instantiate(wasmWithCustomPayload([...linkedStackHigh, ...marker, ...linkedStackHigh]));
    const heap = new Uint8Array(256);
    heap.set(rastate([1]), 64);
    class Manager {
      Module = {
        HEAPU8: heap,
        _save_state_info: () => 24,
        UTF8ToString: () => `${rastate([1]).byteLength}|64|1`,
        _free: () => undefined,
      };
      functions = { loadState: vi.fn() };
      FS = {
        analyzePath: () => ({ exists: false }), mkdir: () => undefined,
        writeFile: vi.fn(), unlink: vi.fn(),
      };
      toggleMainLoop = vi.fn();
    }
    const manager = new Manager();
    installation.prepare({ on: () => undefined, gameManager: manager });
    const restore = (manager as Manager & {
      loadExplicitStateAndWait: (state: Uint8Array) => Promise<void>;
    }).loadExplicitStateAndWait(Uint8Array.of(1, 2, 3));
    await expect(restore).rejects.toThrow("PLAYER_STATE_UNAVAILABLE");
    installation.cleanup();
    vi.useRealTimers();
  });

  it("rejects an in-flight readiness wait when the Player session ends", async () => {
    vi.useFakeTimers();
    const installation = installDOSBoxPureStateCompatibility(window);
    await window.WebAssembly.instantiate(wasmWithCustomPayload([...linkedStackHigh, ...marker, ...linkedStackHigh]));
    class Manager {
      Module = {
        HEAPU8: new Uint8Array(32),
        _save_state_info: () => 4,
        UTF8ToString: () => "Error writing data||0",
        _free: () => undefined,
      };
      functions = { loadState: vi.fn() };
      FS = {
        analyzePath: () => ({ exists: false }),
        mkdir: vi.fn(),
        writeFile: vi.fn(),
        unlink: vi.fn(),
      };
      toggleMainLoop = vi.fn();
    }
    const manager = new Manager();
    installation.prepare({ on: () => undefined, gameManager: manager });
    const restore = (manager as Manager & {
      loadExplicitStateAndWait: (state: Uint8Array) => Promise<void>;
    }).loadExplicitStateAndWait(rastate([1]));

    installation.cleanup();
    await expect(restore).rejects.toThrow("PLAYER_SESSION_ENDED");
    vi.useRealTimers();
  });
});
