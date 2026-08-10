import { afterEach, describe, expect, it, vi } from "vitest";
import { adapterID, captureManualScreenshot, captureManualState, mountEmulatorJS, scheduleStartupActions, switchDisc, type PlayerConfig } from "./ejs-4.2.3-v2";

const config: PlayerConfig = {
  launchId: "01980000-0000-7000-8000-000000000001",
  emulatorjsVersion: "4.2.3",
  playerAdapterId: adapterID,
  core: "mgba",
  runtimeCore: "mgba",
  coreName: "mGBA",
  emulatorGameId: 1004,
  gameName: "retrom-1",
  gameTitle: "Sudoku",
  platformName: "Game Boy Advance",
  runtimeBaseUrl: "/runtime/emulatorjs/4.2.3/data/",
  loaderUrl: "/runtime/emulatorjs/4.2.3/data/loader.js",
  gameUrl: "/runtime/launches/id/game/game.gba",
  biosUrl: null,
  parentUrl: null,
  stateUrl: null,
  persistentSaveMode: "SINGLE_FILE",
  persistentSaveUrl: "/runtime/launches/01980000-0000-7000-8000-000000000001/persistent-save",
  inputMode: "STANDARD",
  startupActions: [],
  requiresThreads: false,
  runtimePathOverrides: { "mgba-wasm.data": "/runtime/emulatorjs/4.2.3/data/cores/mgba-wasm.data" },
  defaultCoreOptions: { webgl2Enabled: "enabled" },
  externalFiles: {},
  returnTo: "/library"
};

describe("EmulatorJS adapter", () => {
  afterEach(() => {
    document.querySelectorAll("script[data-retrom-loader]").forEach((node) => node.remove());
    window.EJS_emulator = undefined;
    Reflect.deleteProperty(window, "EJS_GameManager");
  });

  it("rejects an unregistered runtime without mutating the document", () => {
    const target = document.createElement("div");
    expect(() => mountEmulatorJS({ ...config, emulatorjsVersion: "4.2.4" }, target)).toThrow("PLAYER_ADAPTER_MISMATCH");
    expect(document.querySelector("script[data-retrom-loader]")).toBeNull();
  });

  it("maps validated config into the 4.2.3 globals and same-origin loader", () => {
    const target = document.createElement("div");
    const cleanup = mountEmulatorJS(config, target);
    expect(window.EJS_core).toBe(config.runtimeCore);
    expect(window.EJS_gameUrl).toBe(config.gameUrl);
    expect(window.EJS_externalFiles).toEqual({});
    expect(window.EJS_Buttons).toEqual({ exitEmulation: false });
    expect(document.querySelector<HTMLScriptElement>("script[data-retrom-loader]")?.src).toContain(config.loaderUrl);
    cleanup();
  });

  it("defers 4.3 DOS startup until the whole-archive mode is installed", async () => {
    const target = document.createElement("div");
    const dosConfig: PlayerConfig = {
      ...config,
      emulatorjsVersion: "4.3.0-pre",
      playerAdapterId: "ejs-4.3.0-pre-v1",
      core: "dosbox_pure",
      runtimeCore: "dosbox_pure",
      dosEntry: "GAMES/DOOM.EXE",
      runtimeBaseUrl: "/runtime/emulatorjs/4.3.0-pre/data/",
      loaderUrl: "/runtime/emulatorjs/4.3.0-pre/data/loader.js",
      runtimePathOverrides: { "dosbox_pure-thread-wasm.data": "/runtime/emulatorjs/4.3.0-pre/data/cores/dosbox_pure-thread-wasm.data" },
      defaultCoreOptions: {},
      externalFiles: {}
    };
    const cleanup = mountEmulatorJS(dosConfig, target);
    expect(window.EJS_startOnLoaded).toBe(false);
    expect(window.EJS_dontExtractRom).toBe(true);
    expect(window.EJS_disableBatchBootup).toBe(true);
    const dontExtractIfCore: string[] = [];
    window.EJS_emulator = { on: () => undefined, downloadType: { rom: { dontExtractIfCore } } };
    window.EJS_ready?.();
    const start = document.createElement("button");
    start.className = "ejs_start_button";
    const click = vi.spyOn(start, "click");
    document.body.append(start);
    expect(dontExtractIfCore).toEqual(["dosbox_pure"]);
    await vi.waitFor(() => expect(click).toHaveBeenCalledOnce());
    cleanup();
    start.remove();
  });

  it("accepts only launch-scoped BIOS external files", () => {
    const target = document.createElement("div");
    const biosConfig = {
      ...config,
      externalFiles: {
        "/retroarch/userdata/system/bios7.bin": `/runtime/launches/${config.launchId}/external-files/bios7.bin`
      }
    };
    const cleanup = mountEmulatorJS(biosConfig, target);
    expect(window.EJS_externalFiles).toEqual(biosConfig.externalFiles);
    cleanup();
    expect(() => mountEmulatorJS({ ...biosConfig, externalFiles: { "/../bios7.bin": biosConfig.externalFiles["/retroarch/userdata/system/bios7.bin"] } }, target)).toThrow("PLAYER_EXTERNAL_FILES_INVALID");
  });

  it("normalizes 4.2.3 external ArrayBuffer writes before the runtime starts", () => {
    const target = document.createElement("div");
    const multiDiscConfig: PlayerConfig = {
      ...config,
      externalFiles: {
        "/disc-001.chd": `/runtime/launches/${config.launchId}/external-files/disc-001.chd`
      }
    };
    const cleanup = mountEmulatorJS(multiDiscConfig, target);
    const writes: Array<{ path: string; data: unknown }> = [];
    class GameManager {
      writeFile(path: string, data: unknown) {
        writes.push({ path, data });
      }
    }
    window.EJS_GameManager = GameManager;
    const manager = new GameManager();
    const arrayBuffer = Uint8Array.from([1, 2, 3]).buffer;
    const typedBytes = Uint8Array.from([4, 5]);
    manager.writeFile("/disc-001.chd", arrayBuffer);
    manager.writeFile("/already-typed.bin", typedBytes);
    expect(writes[0]).toEqual({ path: "/disc-001.chd", data: Uint8Array.from([1, 2, 3]) });
    expect(writes[0].data).toBeInstanceOf(Uint8Array);
    expect(writes[1]).toEqual({ path: "/already-typed.bin", data: typedBytes });
    expect(writes[1].data).toBe(typedBytes);
    cleanup();
  });

  it("initializes the EmulatorJS settings object before exposing a validated disc runtime", () => {
    const target = document.createElement("div");
    const multiDiscConfig: PlayerConfig = {
      ...config,
      core: "yabause",
      runtimeCore: "yabause",
      gameUrl: `/runtime/launches/${config.launchId}/game/playlist.m3u`,
      stateUrl: `/runtime/launches/${config.launchId}/state`,
      runtimePathOverrides: { "yabause-wasm.data": "/runtime/emulatorjs/4.2.3/data/cores/yabause-wasm.data" },
      externalFiles: {
        "/disc-001.chd": `/runtime/launches/${config.launchId}/external-files/disc-001.chd`,
        "/disc-002.chd": `/runtime/launches/${config.launchId}/external-files/disc-002.chd`
      },
      discSet: {
        contentKind: "MULTI_DISC_M3U_V1",
        count: 2,
        initialDiscIndex: 0,
        entries: [
          { index: 0, label: "光盘 1", virtualPath: "/disc-001.chd" },
          { index: 1, label: "光盘 2", virtualPath: "/disc-002.chd" }
        ]
      }
    };
    const onReady = vi.fn((instance) => expect(instance.allSettings).toEqual({}));
    const cleanup = mountEmulatorJS(multiDiscConfig, target, { onReady });
    expect(window.EJS_loadStateURL).toBeUndefined();
    window.EJS_emulator = {
      on: () => undefined,
      gameManager: { getDiskCount: () => 2, getCurrentDisk: () => 0, setCurrentDisk: () => undefined }
    };
    window.EJS_ready?.();
    expect(onReady).toHaveBeenCalledOnce();
    cleanup();

    const invalidSettings = {
      on: () => undefined,
      gameManager: { getDiskCount: () => 2, getCurrentDisk: () => 0, setCurrentDisk: () => undefined }
    };
    Reflect.set(invalidSettings, "allSettings", []);
    window.EJS_emulator = invalidSettings;
    expect(() => window.EJS_ready?.()).toThrow("PLAYER_DISC_SETTINGS_INVALID");

    expect(() => mountEmulatorJS({
      ...multiDiscConfig,
      discSet: { ...multiDiscConfig.discSet!, entries: [
        { index: 0, label: "光盘 2", virtualPath: "/disc-001.chd" },
        { index: 1, label: "光盘 1", virtualPath: "/disc-002.chd" }
      ] }
    }, target)).toThrow("PLAYER_DISC_SET_INVALID");
  });

  it("switches a disc only after a successful runtime readback", () => {
    let current = 0;
    const setCurrentDisk = vi.fn((index: number) => { current = index; });
    const instance = {
      on: () => undefined,
      gameManager: { getDiskCount: () => 3, getCurrentDisk: () => current, setCurrentDisk }
    };
    expect(switchDisc(instance, 1, 3)).toEqual({ count: 3, currentIndex: 1 });
    expect(setCurrentDisk).toHaveBeenCalledOnce();
    expect(switchDisc(instance, 1, 3)).toEqual({ count: 3, currentIndex: 1 });
    expect(setCurrentDisk).toHaveBeenCalledOnce();
    expect(() => switchDisc({ ...instance, gameManager: { ...instance.gameManager, getDiskCount: () => -1 } }, 0, 3)).toThrow("PLAYER_DISC_SET_INVALID");
  });

  it("does not schedule startup input when the restore boundary fails closed", () => {
    vi.useFakeTimers();
    const target = document.createElement("div");
    const simulateInput = vi.fn();
    const cleanup = mountEmulatorJS({ ...config, startupActions: [{ event: "GAME_START", kind: "PRESS_CONTROL", delayMs: 1, player: 0, control: 0, durationMs: 1 }] }, target, { onGameStart: () => false });
    window.EJS_emulator = { on: () => undefined, gameManager: { simulateInput } };
    window.EJS_onGameStart?.();
    vi.runAllTimers();
    expect(simulateInput).not.toHaveBeenCalled();
    cleanup();
    vi.useRealTimers();
  });

  it("treats NONE persistent saves as an explicit capability while keeping state callbacks", () => {
    const target = document.createElement("div");
    const onSaveState = vi.fn();
    const onSaveSave = vi.fn();
    const cleanup = mountEmulatorJS(
      { ...config, persistentSaveMode: "NONE", persistentSaveUrl: null },
      target,
      { onSaveState, onSaveSave }
    );
    expect(window.EJS_onSaveState).toBe(onSaveState);
    expect(window.EJS_onSaveSave).toBeUndefined();
    cleanup();
    expect(() => mountEmulatorJS({ ...config, persistentSaveMode: "NONE" }, target)).toThrow("PLAYER_PERSISTENT_CAPABILITY_INVALID");
  });

  it("presses and releases bounded startup controls exactly once", () => {
    vi.useFakeTimers();
    const receivers: unknown[] = [];
    const simulateInput = vi.fn(function (this: unknown) { receivers.push(this); });
    const gameManager = { simulateInput };
    const startupConfig: PlayerConfig = {
      ...config,
      startupActions: [{ event: "GAME_START", kind: "PRESS_CONTROL", delayMs: 2000, player: 0, control: 0, durationMs: 120 }]
    };
    const cleanup = scheduleStartupActions(startupConfig, { on: () => undefined, gameManager });
    vi.advanceTimersByTime(1999);
    expect(simulateInput).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(simulateInput).toHaveBeenLastCalledWith(0, 0, 1);
    expect(receivers.at(-1)).toBe(gameManager);
    vi.advanceTimersByTime(120);
    expect(simulateInput).toHaveBeenLastCalledWith(0, 0, 0);
    cleanup();
    expect(simulateInput).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
  });

  it("captures the running canvas before normalizing the paused manual state", async () => {
    const screenshot = new Blob(["png"], { type: "image/png" });
    const state = Uint8Array.from([1, 2, 3]);
    const takeScreenshot = vi.fn(async () => ({ blob: screenshot, format: "png" }));
    const instance = {
      on: () => undefined,
      capture: { photo: { source: "canvas", format: "png", upscale: 2 } },
      gameManager: { getState: () => state },
      takeScreenshot
    };
    const capture = await captureManualScreenshot(instance);
    const payload = captureManualState(instance, capture);
    expect(takeScreenshot).toHaveBeenCalledWith("canvas", "png", 2);
    expect(payload.format).toBe("png");
    expect(payload.screenshot.type).toBe("image/png");
    expect(payload.screenshot).toBe(screenshot);
    expect(payload.state).toEqual(state);
    expect(payload.state).not.toBe(state);
  });

  it("rejects unavailable or empty screenshots", async () => {
    const state = Uint8Array.from([1, 2, 3]);
    await expect(captureManualScreenshot({ on: () => undefined, gameManager: { getState: () => state } })).rejects.toThrow("PLAYER_SCREENSHOT_UNAVAILABLE");
    await expect(captureManualScreenshot({ on: () => undefined, gameManager: { getState: () => state }, takeScreenshot: async () => ({ blob: new Blob(), format: "png" }) })).rejects.toThrow("PLAYER_SCREENSHOT_EMPTY");
    expect(() => captureManualState({ on: () => undefined, gameManager: { getState: () => state } }, { screenshot: new Blob(), format: "png" })).toThrow("PLAYER_SCREENSHOT_EMPTY");
  });
});
