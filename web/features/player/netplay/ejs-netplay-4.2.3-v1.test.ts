import { afterEach, describe, expect, it, vi } from "vitest";
import type { EmulatorInstance } from "../adapters/ejs-4.2.3-v2";
import { checkpointCoreStateBytes, coreStateBytes, EJSNetplayFrameBridge, installEmulatorJs423NetplayCompatibility } from "./ejs-netplay-4.2.3-v1";

function raState(core: number[], envelope: number[] = []) {
  const envelopePadded = (envelope.length + 7) & ~7;
  const corePadded = (core.length + 7) & ~7;
  const envelopeLength = envelope.length ? 8 + envelopePadded : 0;
  const state = new Uint8Array(8 + envelopeLength + 8 + corePadded + 8);
  state.set(new TextEncoder().encode("RASTATE")); state[7] = 1;
  if (envelope.length) {
    state.set(new TextEncoder().encode("META"), 8);
    new DataView(state.buffer).setUint32(12, envelope.length, true);
    state.set(envelope, 16);
  }
  const coreOffset = 8 + envelopeLength;
  state.set(new TextEncoder().encode("MEM "), coreOffset);
  new DataView(state.buffer).setUint32(coreOffset + 4, core.length, true);
  state.set(core, coreOffset + 8);
  state.set(new TextEncoder().encode("END "), coreOffset + 8 + corePadded);
  return state;
}

describe("EmulatorJS 4.2.3 netplay bridge", () => {
  afterEach(() => {
    vi.useRealTimers();
    Reflect.deleteProperty(window, "EJS_Runtime");
    Reflect.deleteProperty(window, "EJS_GameManager");
    Reflect.deleteProperty(window, "__RETROM_POST_MAIN_LOOP__");
  });

  it("extracts only the MEM chunk and rejects incompatible state containers", () => {
    expect([...coreStateBytes(raState([1, 2, 3]))]).toEqual([1, 2, 3]);
    expect(() => coreStateBytes(new Uint8Array([1, 2, 3]))).toThrow("STATE_INVALID");
  });

  it("excludes only the Neo Geo YM2610/AY8910 audio state from FBNeo checkpoints", () => {
    const core = new Uint8Array(2_200).fill(7);
    const rtcOffset = 2_000;
    const view = new DataView(core.buffer);
    [12, 34, 2, 14, 8, 26, 5, 0, 0].forEach((value, index) => view.setUint32(rtcOffset + index * 4, value, true));
    view.setUint32(rtcOffset + 44, 0, true);
    view.setUint32(rtcOffset + 60, 0x00010100, true);
    view.setUint32(rtcOffset + 64, 12_000_000, true);

    const projected = checkpointCoreStateBytes(core, "fbneo-423-v1");
    expect(projected).not.toBe(core);
    expect([...projected.subarray(0, 4)]).toEqual(Array(4).fill(7));
    expect([...projected.subarray(4, 1_592)]).toEqual(Array(1_588).fill(0));
    expect(projected[1_592]).toBe(7);
    expect([...projected.subarray(rtcOffset - 104, rtcOffset)]).toEqual(Array(104).fill(0));
    expect(projected[rtcOffset - 105]).toBe(7);
    expect(projected[rtcOffset]).toBe(12);
    expect(checkpointCoreStateBytes(core, "fceumm-423-v1")).toBe(core);
  });

  it("keeps strict FBNeo checkpoint bytes when a unique Neo Geo RTC layout is absent", () => {
    const core = new Uint8Array(320).fill(7);
    expect(checkpointCoreStateBytes(core, "fbneo-423-v1")).toBe(core);
  });

  it("intercepts local controls and applies all canonical players for one exact frame", async () => {
    const native: Array<[number, number, number]> = [];
    const publicInput = vi.fn();
    let frame = 3;
    const manager = {
      getState: () => raState([1]),
      getFrameNum: () => frame,
      loadState: () => undefined,
      loadStateAndWait: async () => ({ byteExact: true }),
      runNetplayFrame: async () => { frame += 1; return frame; },
      simulateInput: publicInput,
      functions: { simulateInput: (player: number, control: number, value: number) => native.push([player, control, value]) },
      toggleMainLoop: () => undefined,
    };
    const runtime: EmulatorInstance = { gameManager: manager, paused: false, muted: false, on: () => undefined };
    const bridge = new EJSNetplayFrameBridge(runtime);
    manager.simulateInput(0, 3, 1);
    expect(bridge.sampleLocalControls()[3]).toBe(1);
    expect(native).toEqual([]);
    const inputs = Array.from({ length: 4 }, () => Array<number>(24).fill(0)) as [number[], number[], number[], number[]];
    inputs[0][6] = 1;
    inputs[1][7] = 1;
    await bridge.runNetplayFrame(inputs);
    expect(native).toHaveLength(96);
    expect(native[0]).toEqual([0, 0, 0]);
    expect(native).toContainEqual([0, 6, 1]);
    expect(native).toContainEqual([1, 7, 1]);
    expect(native).not.toContainEqual([0, 7, 1]);
    expect(native).not.toContainEqual([1, 6, 1]);
    expect(native.at(-1)).toEqual([3, 23, 0]);
    bridge.close();
    manager.simulateInput(0, 3, 0);
    expect(publicInput).toHaveBeenCalledWith(0, 3, 0);
  });

  it("accepts a native recapture whose RASTATE envelope changed when the core bytes match", async () => {
    const expected = raState([4, 5, 6], [1]);
    const recaptured = raState([4, 5, 6], [2]);
    const manager = {
      getState: () => new Uint8Array(recaptured),
      getFrameNum: () => 0,
      loadStateAndWait: async () => ({ byteExact: false }),
      runNetplayFrame: async () => 1,
      simulateInput: () => undefined,
      functions: { simulateInput: () => undefined },
      toggleMainLoop: () => undefined,
    };
    const runtime: EmulatorInstance = { gameManager: manager, paused: false, muted: false, on: () => undefined };
    const bridge = new EJSNetplayFrameBridge(runtime);

    await expect(bridge.loadStateAndWait(expected)).resolves.toBeUndefined();
  });

  it("reports a core mismatch for transfer diagnostics while strict rollback loads fail closed", async () => {
    const expected = raState([4, 5, 6]);
    const manager = {
      getState: () => raState([4, 9, 6]),
      getFrameNum: () => 0,
      loadStateAndWait: async () => ({ byteExact: false }),
      runNetplayFrame: async () => 1,
      simulateInput: () => undefined,
      functions: { simulateInput: () => undefined },
      toggleMainLoop: () => undefined,
    };
    const runtime: EmulatorInstance = { gameManager: manager, paused: false, muted: false, on: () => undefined };
    const bridge = new EJSNetplayFrameBridge(runtime);

    await expect(bridge.loadStateForTransfer(expected)).resolves.toMatchObject({
      byteExact: false, coreExact: false, expectedCoreBytes: 3, recapturedCoreBytes: 3, firstCoreMismatch: 1,
    });
    await expect(bridge.loadStateAndWait(expected)).rejects.toThrow("STATE_INVALID");
  });

  it("patches constructors before startup, keeps saves in memory, and waits for native state completion", async () => {
    const originalFetch = window.fetch;
    const requests: string[] = [];
    window.fetch = vi.fn(async (input: RequestInfo | URL) => {
      requests.push(String(input));
      return new Response("ok");
    });
    const cleanup = installEmulatorJs423NetplayCompatibility(window);
    let runtimeConfig: { postMainLoop?: () => void; print?: (...args: unknown[]) => void } = {};
    class GameManager {
      paths: string[] = [];
      frame = 0;
      nativeLoadCalls = 0;
      alterRecapturedByte = false;
      pendingState = Uint8Array.from([]);
      state = Uint8Array.from([1, 2, 3]);
      functions = {
        loadState: () => {
          this.nativeLoadCalls += 1;
          window.setTimeout(() => {
            this.state = new Uint8Array(this.pendingState);
            if (this.alterRecapturedByte) this.state[0] = (this.state[0] ?? 0) ^ 1;
            runtimeConfig.print?.('[INFO] [State] Loading state "game.state"');
          }, 0);
          return 1;
        },
      };
      FS = {
        open: (...args: [string, string]) => { void args; return { flags: 0 }; },
        close: (stream: { flags: number }) => { void stream; },
        unlink: () => undefined,
        writeFile: (_path: string, state: Uint8Array) => { this.pendingState = new Uint8Array(state); },
      };
      mountFileSystems() { throw new Error("IDBFS_MUST_NOT_MOUNT"); }
      mkdir(path: string) { this.paths.push(path); }
      getFrameNum() { return this.frame; }
      getState() { return new Uint8Array(this.state); }
      loadState() { throw new Error("PUBLIC_LOAD_STATE_MUST_NOT_BE_USED_FOR_ROLLBACK"); }
      toggleMainLoop(running: boolean) {
        if (running) window.setTimeout(() => { this.frame += 1; runtimeConfig.postMainLoop?.(); }, 0);
      }
    }
    Reflect.set(window, "EJS_GameManager", GameManager);
    Reflect.set(window, "EJS_Runtime", (config: typeof runtimeConfig) => { runtimeConfig = config; return {}; });
    const runtimeFactory = Reflect.get(window, "EJS_Runtime") as (config: typeof runtimeConfig) => unknown;
    runtimeFactory({});
    const manager = new GameManager() as GameManager & {
      loadStateAndWait: (state: Uint8Array) => Promise<{ byteExact: boolean }>;
      runNetplayFrame: () => Promise<number>;
    };
    await manager.mountFileSystems();
    expect(manager.paths).toEqual(["/data", "/data/saves"]);
    expect(await manager.runNetplayFrame()).toBe(1);
    await expect(manager.loadStateAndWait(Uint8Array.from([9, 8, 7]))).resolves.toEqual({ byteExact: true });
    expect(manager.state).toEqual(Uint8Array.from([9, 8, 7]));
    await expect(manager.loadStateAndWait(Uint8Array.from([9, 8, 7]))).resolves.toEqual({ byteExact: true });
    expect(manager.nativeLoadCalls).toBe(2);
    manager.alterRecapturedByte = true;
    await expect(manager.loadStateAndWait(Uint8Array.from([9, 8, 7]))).resolves.toEqual({ byteExact: false });
    expect(manager.nativeLoadCalls).toBe(3);
    const version = await window.fetch("https://cdn.emulatorjs.org/stable/data/version.json");
    expect(await version.json()).toEqual({ version: "4.2.3", current_version: "4.2.3" });
    await window.fetch("/game.zip");
    expect(requests).toEqual(["/game.zip"]);
    cleanup();
    window.fetch = originalFetch;
  });

  it("fails closed when required native APIs are unavailable", () => {
    const runtime: EmulatorInstance = { on: () => undefined, gameManager: {} };
    expect(() => new EJSNetplayFrameBridge(runtime)).toThrow("NETPLAY_RUNTIME_COMPATIBILITY_UNAVAILABLE");
  });
});
