import { afterEach, describe, expect, it, vi } from "vitest";
import {
  installEmulatorJs423StateRestoreCompatibility,
  requiresExplicitStateRestore,
} from "./explicit-state-restore";

const originalFetch = window.fetch;

afterEach(() => {
  vi.useRealTimers();
  window.fetch = originalFetch;
  Reflect.deleteProperty(window, "EJS_GameManager");
  Reflect.deleteProperty(window, "EJS_Runtime");
});

describe("EmulatorJS 4.2.3 state restore compatibility", () => {
  it("defers every explicitly selected 4.2.3 state regardless of core", () => {
    expect(requiresExplicitStateRestore({ emulatorjsVersion: "4.2.3", stateUrl: "/state" })).toBe(true);
    expect(requiresExplicitStateRestore({ emulatorjsVersion: "4.2.3", stateUrl: null })).toBe(false);
    expect(requiresExplicitStateRestore({ emulatorjsVersion: "4.3.0-pre", runtimeCore: "azahar", stateUrl: "/state" })).toBe(false);
    expect(requiresExplicitStateRestore({ emulatorjsVersion: "4.3.0-pre", runtimeCore: "dosbox_pure", stateUrl: "/state" })).toBe(true);
  });

  it("waits for core state readiness and native completion before resolving", async () => {
    vi.useFakeTimers();
    const upstreamRequests: string[] = [];
    window.fetch = vi.fn(async (input: RequestInfo | URL) => {
      upstreamRequests.push(String(input));
      return new Response("ok");
    });
    const cleanup = installEmulatorJs423StateRestoreCompatibility(window, { waitForSerializable: true });
    let runtimeConfig: { print?: (...args: unknown[]) => void } = {};
    const loop = vi.fn();
    const removed: string[] = [];
    const files = new Map<string, Uint8Array>();
    class GameManager {
      readinessChecks = 0;
      nativeLoads = 0;
      functions = {
        saveStateInfo: () => {
          this.readinessChecks += 1;
          return this.readinessChecks < 3 ? "Error writing data|0|0" : "1|0|1";
        },
        loadState: () => {
          this.nativeLoads += 1;
          window.setTimeout(() => runtimeConfig.print?.('[INFO] [State]: Loading state "game.state", 3 bytes.'), 0);
        },
      };
      FS = {
        unlink: (path: string) => {
          removed.push(path);
          if (!files.delete(path)) {throw new Error("ENOENT");}
        },
        writeFile: (path: string, state: Uint8Array) => files.set(path, new Uint8Array(state)),
      };
      toggleMainLoop(running: boolean) { loop(running); }
    }
    Reflect.set(window, "EJS_GameManager", GameManager);
    Reflect.set(window, "EJS_Runtime", (config: typeof runtimeConfig) => { runtimeConfig = config; return {}; });
    const runtimeFactory = Reflect.get(window, "EJS_Runtime") as (config: typeof runtimeConfig) => unknown;
    runtimeFactory({});
    const manager = new GameManager() as GameManager & {
      loadExplicitStateAndWait: (state: Uint8Array) => Promise<void>;
    };

    const restore = manager.loadExplicitStateAndWait(Uint8Array.of(9, 8, 7));
    expect(manager.nativeLoads).toBe(0);
    await vi.advanceTimersByTimeAsync(150);
    await expect(restore).resolves.toBeUndefined();
    expect(manager.readinessChecks).toBe(3);
    expect(manager.nativeLoads).toBe(1);
    expect(loop.mock.calls.at(-1)).toEqual([false]);
    expect(files.size).toBe(0);
    expect(removed).toContain("/game.state");

    const version = await window.fetch("https://cdn.emulatorjs.org/stable/data/version.json");
    expect(await version.json()).toEqual({ version: "4.2.3", current_version: "4.2.3" });
    await window.fetch("/game.iso");
    expect(upstreamRequests).toEqual(["/game.iso"]);
    cleanup();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("rejects when RetroArch reports that the core refused the state", async () => {
    vi.useFakeTimers();
    const cleanup = installEmulatorJs423StateRestoreCompatibility(window, { waitForSerializable: true });
    let runtimeConfig: { print?: (...args: unknown[]) => void; printErr?: (...args: unknown[]) => void } = {};
    class GameManager {
      functions = {
        saveStateInfo: () => "1|0|1",
        loadState: () => window.setTimeout(() => {
          runtimeConfig.print?.('[INFO] [State]: Loading state "game.state", 1 bytes.');
          runtimeConfig.printErr?.('[ERROR] [State]: Failed to load state from "game.state".');
        }, 0),
      };
      FS = { unlink: () => undefined, writeFile: () => undefined };
      toggleMainLoop() { return undefined; }
    }
    Reflect.set(window, "EJS_GameManager", GameManager);
    Reflect.set(window, "EJS_Runtime", (config: typeof runtimeConfig) => { runtimeConfig = config; return {}; });
    (Reflect.get(window, "EJS_Runtime") as (config: typeof runtimeConfig) => unknown)({});
    const manager = new GameManager() as GameManager & {
      loadExplicitStateAndWait: (state: Uint8Array) => Promise<void>;
    };

    const restore = manager.loadExplicitStateAndWait(Uint8Array.of(9));
    const failure = expect(restore).rejects.toThrow("PLAYER_SAVE_STATE_RESTORE_FAILED");
    await vi.advanceTimersByTimeAsync(1);
    await failure;
    cleanup();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("loads a user-selected state after its serialization layout is ready", async () => {
    vi.useFakeTimers();
    const cleanup = installEmulatorJs423StateRestoreCompatibility(window, { waitForSerializable: true });
    let runtimeConfig: { print?: (...args: unknown[]) => void } = {};
    const readiness = vi.fn(() => "2|0|1");
    let frame = 0;
    const nativeLoad = vi.fn(() => window.setTimeout(() => {
      runtimeConfig.print?.('[INFO] [State]: Loading state "game.state", 2 bytes.');
    }, 0));
    class GameManager {
      functions = { loadState: nativeLoad, saveStateInfo: readiness };
      FS = { unlink: () => undefined, writeFile: () => undefined };
      getFrameNum() { return frame; }
      toggleMainLoop(running: boolean) { if (running) {frame = 1;} }
    }
    Reflect.set(window, "EJS_GameManager", GameManager);
    Reflect.set(window, "EJS_Runtime", (config: typeof runtimeConfig) => { runtimeConfig = config; return {}; });
    (Reflect.get(window, "EJS_Runtime") as (config: typeof runtimeConfig) => unknown)({});
    const manager = new GameManager() as GameManager & {
      loadExplicitStateAndWait: (state: Uint8Array) => Promise<void>;
    };

    const restore = manager.loadExplicitStateAndWait(Uint8Array.of(1, 2));
    expect(readiness).toHaveBeenCalledTimes(1);
    expect(nativeLoad).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(50);
    expect(readiness).toHaveBeenCalledTimes(2);
    expect(nativeLoad).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    await expect(restore).resolves.toBeUndefined();
    cleanup();
  });
});
