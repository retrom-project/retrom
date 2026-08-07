import { afterEach, describe, expect, it } from "vitest";
import { adapterID, captureManualState, mountEmulatorJS, type PlayerConfig } from "./ejs-4.2.3-v1";

const config: PlayerConfig = {
  launchId: "01980000-0000-7000-8000-000000000001",
  emulatorjsVersion: "4.2.3",
  playerAdapterId: adapterID,
  core: "mgba",
  emulatorGameId: 1004,
  gameName: "retrom-1",
  runtimeBaseUrl: "/runtime/emulatorjs/4.2.3/data/",
  loaderUrl: "/runtime/emulatorjs/4.2.3/data/loader.js",
  gameUrl: "/runtime/launches/id/game/game.gba",
  biosUrl: null,
  parentUrl: null,
  stateUrl: null,
  persistentSaveUrl: "/runtime/launches/launch/persistent-save",
  requiresThreads: false,
  runtimePathOverrides: {},
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
    expect(window.EJS_core).toBe("mgba");
    expect(window.EJS_gameUrl).toBe(config.gameUrl);
    expect(window.EJS_externalFiles).toEqual({});
    expect(document.querySelector<HTMLScriptElement>("script[data-retrom-loader]")?.src).toContain(config.loaderUrl);
    cleanup();
  });

  it("maps only the launch-scoped DOS configuration into the virtual filesystem", () => {
    const target = document.createElement("div");
    const dosConfig: PlayerConfig = {
      ...config,
      core: "dosbox_pure",
      dosEntry: "GAMES/DOOM.EXE",
      defaultCoreOptions: { dosbox_pure_conf: "outside" },
      externalFiles: { "/game.conf": `/runtime/launches/${config.launchId}/dos-config/game.conf` }
    };
    const cleanup = mountEmulatorJS(dosConfig, target);
    expect(window.EJS_externalFiles).toEqual(dosConfig.externalFiles);
    cleanup();
    expect(() => mountEmulatorJS({ ...dosConfig, externalFiles: { "/game.conf": "https://example.test/game.conf" } }, target)).toThrow("PLAYER_EXTERNAL_FILES_INVALID");
  });

  it("normalizes the 4.2.3 state and screenshot APIs into a manual save payload", async () => {
    const screenshot = new Blob(["png"], { type: "image/png" });
    const state = Uint8Array.from([1, 2, 3]);
    const payload = await captureManualState({
      on: () => undefined,
      capture: { photo: { source: "canvas", format: "png", upscale: 2 } },
      gameManager: { getState: () => state },
      takeScreenshot: async (source, format, upscale) => {
        expect([source, format, upscale]).toEqual(["canvas", "png", 2]);
        return { blob: screenshot, format: "png" };
      }
    });
    expect(payload).toEqual({ screenshot, format: "png", state });
    expect(payload.state).not.toBe(state);
  });
});
