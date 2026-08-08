import { afterEach, describe, expect, it, vi } from "vitest";
import { adapterID, captureManualScreenshot, captureManualState, mountEmulatorJS, scheduleStartupActions, type PlayerConfig } from "./ejs-4.2.3-v1";

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
  afterEach(() => document.querySelectorAll("script[data-retrom-loader]").forEach((node) => node.remove()));

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

  it("maps only the launch-scoped DOS configuration into the virtual filesystem", () => {
    const target = document.createElement("div");
    const dosConfig: PlayerConfig = {
      ...config,
      core: "dosbox_pure",
      runtimeCore: "dosbox_pure",
      dosEntry: "GAMES/DOOM.EXE",
      defaultCoreOptions: { dosbox_pure_conf: "outside" },
      externalFiles: { "/game.conf": `/runtime/launches/${config.launchId}/dos-config/game.conf` }
    };
    const cleanup = mountEmulatorJS(dosConfig, target);
    expect(window.EJS_externalFiles).toEqual(dosConfig.externalFiles);
    cleanup();
    expect(() => mountEmulatorJS({ ...dosConfig, externalFiles: { "/game.conf": "https://example.test/game.conf" } }, target)).toThrow("PLAYER_EXTERNAL_FILES_INVALID");
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
    const simulateInput = vi.fn();
    const startupConfig: PlayerConfig = {
      ...config,
      startupActions: [{ event: "GAME_START", kind: "PRESS_CONTROL", delayMs: 2000, player: 0, control: 0, durationMs: 120 }]
    };
    const cleanup = scheduleStartupActions(startupConfig, { on: () => undefined, gameManager: { simulateInput } });
    vi.advanceTimersByTime(1999);
    expect(simulateInput).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(simulateInput).toHaveBeenLastCalledWith(0, 0, 1);
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
