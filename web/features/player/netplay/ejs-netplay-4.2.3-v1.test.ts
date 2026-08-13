import { afterEach, describe, expect, it, vi } from "vitest";
import type { EmulatorInstance } from "../adapters/ejs-4.2.3-v2";
import { coreStateBytes, EJSNetplayFrameBridge, installEmulatorJs423NetplayCompatibility } from "./ejs-netplay-4.2.3-v1";

function raState(core: number[]) {
  const padded = (core.length + 7) & ~7;
  const state = new Uint8Array(8 + 8 + padded + 8);
  state.set(new TextEncoder().encode("RASTATE")); state[7] = 1;
  state.set(new TextEncoder().encode("MEM "), 8);
  new DataView(state.buffer).setUint32(12, core.length, true);
  state.set(core, 16); state.set(new TextEncoder().encode("END "), 16 + padded);
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
    const inputs = [0, 1, 2, 3].map((value) => Array<number>(24).fill(value)) as [number[], number[], number[], number[]];
    await bridge.runNetplayFrame(inputs);
    expect(native).toHaveLength(96);
    expect(native[0]).toEqual([0, 0, 0]);
    expect(native.at(-1)).toEqual([3, 23, 3]);
    bridge.close();
    manager.simulateInput(0, 3, 0);
    expect(publicInput).toHaveBeenCalledWith(0, 3, 0);
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
      pendingState = Uint8Array.from([]);
      state = Uint8Array.from([1, 2, 3]);
      functions = {
        loadState: () => {
          this.nativeLoadCalls += 1;
          window.setTimeout(() => {
            this.state = new Uint8Array(this.pendingState);
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
