import { afterEach, describe, expect, it, vi } from "vitest";
import {
  installEmulatorJs423PspStateRestoreCompatibility,
  requiresExplicitPspStateRestore,
} from "./psp-state-restore";

const originalFetch = window.fetch;

afterEach(() => {
  vi.useRealTimers();
  window.fetch = originalFetch;
  Reflect.deleteProperty(window, "EJS_GameManager");
  Reflect.deleteProperty(window, "EJS_Runtime");
});

describe("PSP state restore compatibility", () => {
  it("only selects explicit restore for EmulatorJS 4.2.3 PPSSPP file-tree launches with a state", () => {
    expect(requiresExplicitPspStateRestore({
      emulatorjsVersion: "4.2.3", persistentSaveMode: "FILE_TREE", stateUrl: "/state",
    })).toBe(true);
    expect(requiresExplicitPspStateRestore({
      emulatorjsVersion: "4.2.3", persistentSaveMode: "FILE_TREE", stateUrl: null,
    })).toBe(false);
    expect(requiresExplicitPspStateRestore({
      emulatorjsVersion: "4.3.0-pre", persistentSaveMode: "FILE_TREE", stateUrl: "/state",
    })).toBe(false);
    expect(requiresExplicitPspStateRestore({
      emulatorjsVersion: "4.2.3", persistentSaveMode: "SINGLE_FILE", stateUrl: "/state",
    })).toBe(false);
  });

  it("waits for PPSSPP state readiness and native completion before resolving", async () => {
    vi.useFakeTimers();
    const upstreamRequests: string[] = [];
    window.fetch = vi.fn(async (input: RequestInfo | URL) => {
      upstreamRequests.push(String(input));
      return new Response("ok");
    });
    const cleanup = installEmulatorJs423PspStateRestoreCompatibility(window);
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
          if (!files.delete(path)) throw new Error("ENOENT");
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
      loadPspStateAndWait: (state: Uint8Array) => Promise<void>;
    };

    const restore = manager.loadPspStateAndWait(Uint8Array.of(9, 8, 7));
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

  it("rejects when RetroArch reports that PPSSPP refused the state", async () => {
    vi.useFakeTimers();
    const cleanup = installEmulatorJs423PspStateRestoreCompatibility(window);
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
      loadPspStateAndWait: (state: Uint8Array) => Promise<void>;
    };

    const restore = manager.loadPspStateAndWait(Uint8Array.of(9));
    const failure = expect(restore).rejects.toThrow("PLAYER_SAVE_STATE_RESTORE_FAILED");
    await vi.advanceTimersByTimeAsync(0);
    await failure;
    cleanup();
    expect(vi.getTimerCount()).toBe(0);
  });
});
