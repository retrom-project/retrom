import { afterEach, describe, expect, it, vi } from "vitest";
import { adapterID, captureManualScreenshot, captureManualState, captureReviewScreenshot, coreFramebufferNeedsCanvasOrientation, mountEmulatorJS, scheduleStartupActions, switchDisc, switchDiscPreservingPause, type PlayerConfig } from "./ejs-4.2.3-v2";

const config: PlayerConfig = {
  mode: "single",
  launchId: "01980000-0000-7000-8000-000000000001",
  emulatorjsVersion: "4.2.3",
  playerAdapterId: adapterID,
  core: "mgba",
  runtimeCore: "mgba",
  coreName: "mGBA",
  coreArtifactId: "00000000-0000-4000-8000-000000000001",
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
  persistentSaveMode: "NONE",
  persistentSaveUrl: null,
  inputMode: "STANDARD",
  startupActions: [],
  requiresThreads: false,
  runtimePathOverrides: { "mgba-wasm.data": "/runtime/emulatorjs/4.2.3/data/cores/mgba-wasm.data" },
  defaultCoreOptions: { webgl2Enabled: "enabled" },
  externalFiles: {},
  returnTo: "/library",
  netplay: null
};

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

const originalWindowFetch = window.fetch;

const sevenZipWorkerSource = [
  'function getCFunc(_0x222174){var _0x54cf7b=Module["_"+_0x222174];if(!_0x54cf7b)try{_0x54cf7b=eval("_"+_0x222174)}catch(_0x4b65d1){}}',
  'function cwrap(_0x405d7e,_0x2bdb59,_0x4f818b){var _0x370f8c="generated";return eval(_0x370f8c)}',
].join("");

const zipWorkerSource = [
  'function getCFunc(_0x5d9040){var _0x23b817=Module["_"+_0x5d9040];if(!_0x23b817)try{_0x23b817=eval("_"+_0x5d9040)}catch(_0x2989f0){}}',
  'function cwrap(_0x557d23,_0x36bd20,_0x501373){var _0x6f14b3="generated";return eval(_0x6f14b3)}',
].join("");

function pngWithDimensions(width: number, height: number) {
  const bytes = new Uint8Array(24);
  bytes.set([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a], 0);
  bytes.set([0x49, 0x48, 0x44, 0x52], 12);
  const view = new DataView(bytes.buffer);
  view.setUint32(16, width);
  view.setUint32(20, height);
  return bytes;
}

function pinnedDOSBoxWasmShape() {
  const payload = [
    1, 0x78,
    0xf0, 0xec, 0x80, 0x0e,
    0x23, 0x0c, 0x45, 0x04, 0x40, 0x20, 0x01, 0x41, 0xc0, 0x02,
    0x6a, 0x24, 0x00, 0x20, 0x01, 0x41, 0x10, 0x6a, 0x0f,
    0xf0, 0xec, 0x80, 0x0e,
  ];
  return Uint8Array.of(0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x00, payload.length, ...payload);
}

const fbneoNetplayConfig: PlayerConfig = {
  ...config,
  mode: "netplay",
  core: "fbneo",
  runtimeCore: "fbneo",
  coreName: "FinalBurn Neo",
  coreArtifactId: "01980000-0000-7000-8000-000000000003",
  stateUrl: null,
  persistentSaveMode: "NONE",
  persistentSaveUrl: null,
  runtimePathOverrides: { "fbneo-wasm.data": "/runtime/emulatorjs/4.2.3/data/cores/fbneo-wasm.data" },
  netplay: {
    roomId: "01980000-0000-7000-8000-000000000004",
    sessionId: "01980000-0000-7000-8000-000000000005",
    playerNo: 1,
    runtimeSocketUrl: "/runtime/netplay/rooms/01980000-0000-7000-8000-000000000004/socket",
    netplayProfile: {
      schemaVersion: 1,
      protocolVersion: "retrom-netplay-v1",
      profileId: "fbneo-423-v1",
      emulatorjsVersion: "4.2.3",
      playerAdapterId: "ejs-4.2.3-v2",
      netplayAdapterId: "ejs-netplay-4.2.3-v1",
      coreArtifactId: "01980000-0000-7000-8000-000000000003",
      coreArtifactSha256: "1".repeat(64),
      gameVariantRevisionId: "01980000-0000-7000-8000-000000000006",
      sourceManifestDigest: "2".repeat(64),
      dependencySnapshotDigest: "3".repeat(64),
      defaultCoreOptions: {},
      controlCount: 24,
      maxPlayers: 2,
      maxPredictionFrames: 0,
      maxRollbackFrames: 120,
      checkpointEveryFrames: 120,
      canonicalHistoryFrames: 600,
      maxStateBytes: 1_048_576,
    },
  },
};

describe("EmulatorJS adapter", () => {
  afterEach(() => {
    document.querySelectorAll("script[data-retrom-loader]").forEach((node) => node.remove());
    window.EJS_emulator = undefined;
    Reflect.deleteProperty(window, "EJS_shaders");
    Reflect.deleteProperty(window, "EJS_GameManager");
    Reflect.deleteProperty(window, "EJS_COMPRESSION");
    window.fetch = originalWindowFetch;
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
    expect(window.EJS_DEBUG_XX).toBe(false);
    expect(window.EJS_EXPERIMENTAL_NETPLAY).toBe(false);
    expect(window.EJS_shaders).toMatchObject({
      "retrom-sharp-bilinear": { shader: { type: "text" } },
      "retrom-adaptive-sharpen": { shader: { type: "text" } },
    });
    expect(document.querySelector<HTMLScriptElement>("script[data-retrom-loader]")?.src).toContain(config.loaderUrl);
    cleanup();
  });

  it("uses the FBNeo profile prediction limit and disables non-deterministic hiscores in netplay", () => {
    const target = document.createElement("div");
    const cleanup = mountEmulatorJS(fbneoNetplayConfig, target);
    expect(window.EJS_defaultOptions).toMatchObject({ "fbneo-hiscores": "disabled" });
    cleanup();
    expect(() => mountEmulatorJS({
      ...fbneoNetplayConfig,
      netplay: {
        ...fbneoNetplayConfig.netplay!,
        netplayProfile: { ...fbneoNetplayConfig.netplay!.netplayProfile, maxPredictionFrames: 8 },
      },
    }, target)).toThrow("PLAYER_NETPLAY_CONFIG_INVALID");
  });

  it("defers 4.3 DOS startup until the whole-archive mode is installed", async () => {
    const target = document.createElement("div");
    const cleanup = mountEmulatorJS(dosConfig, target);
    expect(window.EJS_startOnLoaded).toBe(false);
    expect(window.EJS_dontExtractRom).toBe(true);
    expect(window.EJS_disableBatchBootup).toBe(true);
    const dontExtractIfCore: string[] = [];
    await window.WebAssembly.instantiate(pinnedDOSBoxWasmShape());
    class DOSManager { getState() { return Uint8Array.of(1); } }
    window.EJS_emulator = {
      on: () => undefined,
      downloadType: { rom: { dontExtractIfCore } },
      gameManager: new DOSManager(),
    };
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

  it("rewrites the pinned 4.2.3 7z worker without JavaScript eval", async () => {
    const target = document.createElement("div");
    const cleanup = mountEmulatorJS(config, target);
    const workerBlob = new Blob([sevenZipWorkerSource], { type: "application/javascript" });
    class Compression {
      async getWorkerFile(archiveType: string) {
        if (archiveType !== "7z" && archiveType !== "rar") throw new Error("unexpected archive type");
        return workerBlob;
      }
    }
    window.EJS_COMPRESSION = Compression;

    const patched = await new Compression().getWorkerFile("7z");
    const patchedSource = await patched.text();
    expect(patchedSource).not.toContain("eval(");
    expect(patchedSource).toContain('Module["_"+_0x222174]');
    expect(patchedSource).toContain("ccall(_0x405d7e,_0x2bdb59,_0x4f818b,Array.prototype.slice.call(arguments))");
    await expect(new Compression().getWorkerFile("rar")).resolves.toBe(workerBlob);
    cleanup();
  });

  it("rewrites the byte-identical pinned 4.3 DOS core 7z worker without JavaScript eval", async () => {
    const frame = document.createElement("iframe");
    document.body.append(frame);
    const runtimeWindow = frame.contentWindow!;
    expect(runtimeWindow.location.href).toBe("about:blank");
    const target = runtimeWindow.document.createElement("div");
    const fetchMock = vi.fn(async () => new Response(sevenZipWorkerSource, { status: 200, headers: { "Content-Length": "100", ETag: "worker-v1" } }));
    runtimeWindow.fetch = fetchMock;
    const cleanup = mountEmulatorJS(dosConfig, target, {}, runtimeWindow);
    const workerURL = new URL("/runtime/emulatorjs/4.3.0-pre/data/compression/extract7z.js", window.location.href);
    const patched = await runtimeWindow.fetch(workerURL.href);
    const patchedSource = await patched.text();
    expect(patchedSource).not.toContain("eval(");
    expect(patchedSource).toContain('Module["_"+_0x222174]');
    expect(patchedSource).toContain("ccall(_0x405d7e,_0x2bdb59,_0x4f818b,Array.prototype.slice.call(arguments))");
    expect(patched.headers.has("content-length")).toBe(false);
    expect(patched.headers.has("etag")).toBe(false);
    const unrelated = await runtimeWindow.fetch("/runtime/emulatorjs/4.3.0-pre/data/compression/libunrar.js");
    expect(await unrelated.text()).toBe(sevenZipWorkerSource);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    cleanup();
    expect(runtimeWindow.fetch).toBe(fetchMock);
    frame.remove();
  });

  it("rewrites the pinned 4.2.3 zip worker without JavaScript eval", async () => {
    const target = document.createElement("div");
    const cleanup = mountEmulatorJS(config, target);
    const workerBlob = new Blob([zipWorkerSource], { type: "application/javascript" });
    class Compression {
      async getWorkerFile(archiveType: string) {
        if (archiveType !== "zip") throw new Error("unexpected archive type");
        return workerBlob;
      }
    }
    window.EJS_COMPRESSION = Compression;

    const patchedSource = await (await new Compression().getWorkerFile("zip")).text();
    expect(patchedSource).not.toContain("eval(");
    expect(patchedSource).toContain('Module["_"+_0x5d9040]');
    expect(patchedSource).toContain("ccall(_0x557d23,_0x36bd20,_0x501373,Array.prototype.slice.call(arguments))");
    cleanup();
  });

  it("fails closed when a pinned 4.2.3 archive worker shape drifts", async () => {
    const target = document.createElement("div");
    const cleanup = mountEmulatorJS(config, target);
    class Compression {
      async getWorkerFile(archiveType: string) {
        if (archiveType !== "7z" && archiveType !== "zip") throw new Error("unexpected archive type");
        return new Blob(["unexpected-worker-source"], { type: "application/javascript" });
      }
    }
    window.EJS_COMPRESSION = Compression;

    await expect(new Compression().getWorkerFile("7z")).rejects.toThrow("PLAYER_ARCHIVE_COMPATIBILITY_UNAVAILABLE");
    await expect(new Compression().getWorkerFile("zip")).rejects.toThrow("PLAYER_ARCHIVE_COMPATIBILITY_UNAVAILABLE");
    cleanup();
  });

  it("fails closed when the pinned 4.3 archive worker response shape drifts", async () => {
    const target = document.createElement("div");
    window.fetch = vi.fn(async () => new Response("unexpected-worker-source", { status: 200 }));
    const cleanup = mountEmulatorJS(dosConfig, target);
    await expect(window.fetch("/runtime/emulatorjs/4.3.0-pre/data/compression/extract7z.js")).rejects.toThrow("PLAYER_ARCHIVE_COMPATIBILITY_UNAVAILABLE");
    await expect(window.fetch("/runtime/emulatorjs/4.3.0-pre/data/compression/extractzip.js")).rejects.toThrow("PLAYER_ARCHIVE_COMPATIBILITY_UNAVAILABLE");
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

  it("resumes a running core after switching discs and preserves an existing pause", () => {
    const calls: string[] = [];
    let current = 0;
    const instance = {
      paused: false,
      on: () => undefined,
      gameManager: {
        getDiskCount: () => 2,
        getCurrentDisk: () => current,
        setCurrentDisk: (index: number) => { calls.push(`disc:${index}`); current = index; },
        toggleMainLoop: (running: boolean) => { calls.push(`loop:${running}`); },
      },
    };

    expect(switchDiscPreservingPause(instance, 1, 2)).toEqual({ count: 2, currentIndex: 1 });
    expect(calls).toEqual(["loop:false", "disc:1", "loop:true"]);
    expect(instance.paused).toBe(false);

    calls.length = 0;
    instance.paused = true;
    expect(switchDiscPreservingPause(instance, 0, 2)).toEqual({ count: 2, currentIndex: 0 });
    expect(calls).toEqual(["loop:false", "disc:0", "loop:false"]);
    expect(instance.paused).toBe(true);
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

  it("exposes only explicit state callbacks and rejects automatic persistence capabilities", () => {
    const target = document.createElement("div");
    const onSaveState = vi.fn();
    const cleanup = mountEmulatorJS(config, target, { onSaveState });
    expect(window.EJS_onSaveState).toBe(onSaveState);
    expect(window.EJS_onSaveSave).toBeUndefined();
    cleanup();
    expect(() => mountEmulatorJS({
      ...config,
      persistentSaveMode: "SINGLE_FILE",
      persistentSaveUrl: `/runtime/launches/${config.launchId}/persistent-save`,
    }, target)).toThrow("PLAYER_PERSISTENT_CAPABILITY_INVALID");
  });

  it("uses explicit native restore for every selected 4.2.3 state and DOSBox Pure", () => {
    const target = document.createElement("div");
    const restoreCleanup = mountEmulatorJS({ ...config, stateUrl: "/runtime/launches/id/state" }, target);
    expect(window.EJS_loadStateURL).toBeUndefined();
    expect(window.EJS_DEBUG_XX).toBe(true);
    restoreCleanup();
    const loaderCleanup = mountEmulatorJS({ ...dosConfig, stateUrl: "/runtime/launches/id/state" }, target);
    expect(window.EJS_loadStateURL).toBeUndefined();
    expect(window.EJS_DEBUG_XX).toBe(true);
    loaderCleanup();
  });

  it("presses and releases bounded startup controls exactly once", () => {
    vi.useFakeTimers();
    const receivers: unknown[] = [];
    const simulateInput = vi.fn(function (this: unknown) { receivers.push(this); });
    const gameManager = { simulateInput };
    const startupConfig: PlayerConfig = {
      ...config,
      startupActions: [{ event: "GAME_START", kind: "PRESS_CONTROL", delayMs: 25_000, player: 0, control: 3, durationMs: 120 }]
    };
    const cleanup = scheduleStartupActions(startupConfig, { on: () => undefined, gameManager });
    vi.advanceTimersByTime(24_999);
    expect(simulateInput).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(simulateInput).toHaveBeenLastCalledWith(0, 3, 1);
    expect(receivers.at(-1)).toBe(gameManager);
    vi.advanceTimersByTime(120);
    expect(simulateInput).toHaveBeenLastCalledWith(0, 3, 0);
    cleanup();
    expect(simulateInput).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
  });

  it("accepts 30 seconds, rejects 30 seconds plus one ms, and cleanup releases held input", () => {
    vi.useFakeTimers();
    const target = document.createElement("div");
    const atBoundary = { event: "GAME_START" as const, kind: "PRESS_CONTROL" as const, delayMs: 30_000, player: 0, control: 3, durationMs: 120 };
    const mountedCleanup = mountEmulatorJS({ ...config, startupActions: [atBoundary] }, target);
    mountedCleanup();
    expect(() => mountEmulatorJS({ ...config, startupActions: [{ ...atBoundary, delayMs: 30_001 }] }, target)).toThrow("PLAYER_STARTUP_ACTION_INVALID");

    const simulateInput = vi.fn();
    const cleanup = scheduleStartupActions(
      { ...config, startupActions: [{ ...atBoundary, delayMs: 0 }] },
      { on: () => undefined, gameManager: { simulateInput } }
    );
    vi.advanceTimersByTime(0);
    expect(simulateInput).toHaveBeenLastCalledWith(0, 3, 1);
    cleanup();
    expect(simulateInput).toHaveBeenLastCalledWith(0, 3, 0);
    vi.runAllTimers();
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

  it("captures the core framebuffer before using the canvas fallback", async () => {
    const screenshotBytes = Uint8Array.from([137, 80, 78, 71, 13, 10, 26, 10]);
    const requestScreenshot = vi.fn();
    const takeScreenshot = vi.fn();
    const capture = await captureManualScreenshot({
      on: () => undefined,
      gameManager: {
        FS: {
          analyzePath: () => ({ exists: true }),
          mkdir: () => undefined,
          writeFile: () => undefined,
          unlink: vi.fn(),
          stat: vi.fn(),
          readFile: () => screenshotBytes,
        },
        functions: { screenshot: requestScreenshot },
      },
      takeScreenshot,
    });

    expect(requestScreenshot).toHaveBeenCalledOnce();
    expect(takeScreenshot).not.toHaveBeenCalled();
    expect(capture.format).toBe("png");
    expect(capture.screenshot.type).toBe("image/png");
    expect(new Uint8Array(await capture.screenshot.arrayBuffer())).toEqual(screenshotBytes);
  });

  it("uses the displayed canvas when a vertical core framebuffer is rotated", async () => {
    const rawCorePng = pngWithDimensions(256, 224);
    expect(coreFramebufferNeedsCanvasOrientation(rawCorePng, 3 / 4)).toBe(true);
    expect(coreFramebufferNeedsCanvasOrientation(rawCorePng, 4 / 3)).toBe(false);
    const displayed = new Blob(["displayed-upright"], { type: "image/png" });
    const takeScreenshot = vi.fn(async () => ({ blob: displayed, format: "png" }));
    const capture = await captureManualScreenshot({
      on: () => undefined,
      gameManager: {
        FS: {
          analyzePath: () => ({ exists: true }),
          mkdir: () => undefined,
          writeFile: () => undefined,
          unlink: () => undefined,
          stat: () => ({ mode: 0o100666, size: rawCorePng.byteLength }),
          readFile: () => rawCorePng,
        },
        functions: { screenshot: () => undefined },
        getVideoDimensions: () => 3 / 4,
      },
      takeScreenshot,
    });

    expect(takeScreenshot).toHaveBeenCalledWith("canvas", "png", 1);
    expect(capture.screenshot).toBe(displayed);
  });

  it("falls back to the normal canvas capture when a core framebuffer API is unavailable", async () => {
    const screenshot = new Blob(["canvas"], { type: "image/png" });
    const takeScreenshot = vi.fn(async () => ({ blob: screenshot, format: "png" }));
    const capture = await captureReviewScreenshot({ on: () => undefined, takeScreenshot });

    expect(takeScreenshot).toHaveBeenCalledWith("canvas", "png", 1);
    expect(capture.screenshot).toBe(screenshot);
  });

  it("rejects unavailable or empty screenshots", async () => {
    const state = Uint8Array.from([1, 2, 3]);
    await expect(captureManualScreenshot({ on: () => undefined, gameManager: { getState: () => state } })).rejects.toThrow("PLAYER_SCREENSHOT_UNAVAILABLE");
    await expect(captureManualScreenshot({ on: () => undefined, gameManager: { getState: () => state }, takeScreenshot: async () => ({ blob: new Blob(), format: "png" }) })).rejects.toThrow("PLAYER_SCREENSHOT_EMPTY");
    expect(() => captureManualState({ on: () => undefined, gameManager: { getState: () => state } }, { screenshot: new Blob(), format: "png" })).toThrow("PLAYER_SCREENSHOT_EMPTY");
  });
});
