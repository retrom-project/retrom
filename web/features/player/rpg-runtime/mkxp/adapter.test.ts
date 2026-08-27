import { afterEach, describe, expect, it, vi } from "vitest";
import type { Nostalgist } from "nostalgist";
import { rpgValidationGates } from "../../rpg-validation-protocol";
import type { RpgRuntimeConfig } from "../contract";
import { mountMkxp } from "./adapter";
import { encodeMkxpRastate } from "./state";

type MkxpConfig = RpgRuntimeConfig & {
  adapter: Extract<RpgRuntimeConfig["adapter"], { adapterKind: "MKXP_LIBRETRO_WEB" }>;
};

type TestFileSystem = {
  analyzePath(path: string): { exists: boolean };
  mkdirTree(path: string): void;
  readdir(path: string): string[];
  readFile(path: string): Uint8Array<ArrayBufferLike>;
  stat(path: string): { size: number };
  unlink(path: string): void;
  rename(from: string, to: string): void;
  writeFile(path: string, contents: Uint8Array): void;
};

const statePath = "/home/web_user/retroarch/userdata/states/mkxp-z/game.state";
const coreStateRoot = "/home/web_user/retroarch/userdata/states/mkxp-z";
const evidencePath = "/home/web_user/retroarch/userdata/saves/mkxp-z/mkxp-z/Saves/RETROM RPGXP-/retrom-position-v1";
const movedEvidencePath = "/home/web_user/retroarch/userdata/saves/mkxp-z/mkxp-z/Saves/RETROM RPGVX-/retrom-position-v1";
const stateSize = 268435456;
const stateFixture = new Uint8Array(stateSize);
stateFixture.set([0x6d, 0x6b, 0x78, 0x70, 1, 0, 0, 0]);
const originalCrossOriginIsolated = Object.getOwnPropertyDescriptor(window, "crossOriginIsolated");

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  if (originalCrossOriginIsolated) {
    Object.defineProperty(window, "crossOriginIsolated", originalCrossOriginIsolated);
  } else {
    Reflect.deleteProperty(window, "crossOriginIsolated");
  }
});

describe("mkxp runtime mount", () => {
  it("keeps native stderr diagnostics out of the Next development error channel", async () => {
    const harness = createHarness();
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const diagnostics: unknown[] = [];
    const receiveDiagnostic = (event: Event) => diagnostics.push((event as CustomEvent).detail);
    window.addEventListener("retrom:runtime-diagnostic", receiveDiagnostic);

    const mounted = await mountMkxp(mkxpConfig(false), harness.target, null, harness.dependencies);
    const printErr = harness.prepareOptions?.emscriptenModule?.printErr;
    expect(printErr).toBeTypeOf("function");
    printErr?.("[INFO] RetroArch startup");

    expect(consoleError).not.toHaveBeenCalled();
    expect(diagnostics).toEqual([{ runtime: "mkxp-z", message: "[INFO] RetroArch startup" }]);
    window.removeEventListener("retrom:runtime-diagnostic", receiveDiagnostic);
    await mounted.cleanup();
    harness.frame.remove();
  });

  it("does not send the restore hotkey until the position bridge has produced evidence", async () => {
    const harness = createHarness();
    harness.runtime.start.mockImplementation(async () => {
      harness.actions.push("start");
      setTimeout(() => {
        harness.files.set(evidencePath, positionBytes(7, 3, 6, 9, 599));
      }, 500);
    });
    harness.onKeyDown = (code) => {
      harness.actions.push(code);
      if (code === "F4") {harness.files.set(evidencePath, positionBytes(7, 4, 6, 9, 600));}
    };

    const mountPromise = mountMkxp(mkxpConfig(true), harness.target, stateFixture, harness.dependencies);
    await vi.advanceTimersByTimeAsync(499);

    expect(harness.actions).toEqual([`mkdir:${coreStateRoot}`, `write:${statePath}`, "start"]);

    await vi.advanceTimersByTimeAsync(1_000);
    await expect(mountPromise).resolves.toBeDefined();
    expect(harness.actions).toEqual([`mkdir:${coreStateRoot}`, `write:${statePath}`, "start", "F4"]);
    harness.frame.remove();
  });

  it("loads an exact restore through F4 before ready and saves through F2 in the core loop", async () => {
    const harness = createHarness();
    harness.onKeyDown = (code) => {
      harness.actions.push(code);
      if (code === "F4") {harness.files.set(evidencePath, positionBytes(7, 4, 6, 9, 600));}
      if (code === "F2") {harness.writeRuntimeState(encodeMkxpRastate(stateFixture, stateSize));}
    };
    const mountPromise = mountMkxp(mkxpConfig(true), harness.target, stateFixture, harness.dependencies);

    await vi.advanceTimersByTimeAsync(1_000);

    const mounted = await mountPromise;
    expect(harness.actions).toEqual([`mkdir:${coreStateRoot}`, `write:${statePath}`, "start", "F4"]);
    expect(harness.stateAtStart?.byteLength).toBe(stateSize + 24);
    expect(harness.stateAtStart?.slice(0, 24)).toEqual(Uint8Array.of(
      0x52, 0x41, 0x53, 0x54, 0x41, 0x54, 0x45, 1,
      0x4d, 0x45, 0x4d, 0x20, 0, 0, 0, 16,
      0x6d, 0x6b, 0x78, 0x70, 1, 0, 0, 0,
    ));
    expect(mounted.position()).toEqual({ mapId: 7, playerX: 4, playerY: 6, fixtureState: 9 });
    expect(harness.prepareOptions?.retroarchConfig).toMatchObject({
      input_save_state: "f2",
      input_load_state: "f4",
      input_pause_toggle: "f6",
      input_player1_a: "x",
      savestate_file_compression: false,
    });
    expect(harness.canvasIdAtPrepare).toBe("canvas");
    expect(harness.directories).toContain(coreStateRoot);
    expect("state" in (harness.prepareOptions ?? {})).toBe(false);
    expect(mounted.instance.canvas?.ownerDocument).toBe(harness.frame.contentDocument);
    expect(harness.prepareOptions?.element).toBe(mounted.instance.canvas);
    expect(mounted.instance.canvas?.id).toBe("canvas");

    const screenshot = new Blob([Uint8Array.of(1)], { type: "image/png" });
    Object.defineProperty(mounted.instance.canvas, "toBlob", {
      configurable: true,
      value: (callback: BlobCallback) => callback(screenshot),
    });
    expect(await mounted.instance.takeScreenshot?.()).toEqual({ blob: screenshot, format: "png" });

    const getState = mounted.instance.gameManager?.getStateAsync;
    if (!getState) {throw new Error("getStateAsync unavailable");}
    const checkpointPromise = getState();
    await vi.advanceTimersByTimeAsync(1_000);
    const checkpoint = await checkpointPromise;
    expect(checkpoint?.byteLength).toBe(stateSize);
    expect(checkpoint?.slice(0, 8)).toEqual(Uint8Array.of(0x6d, 0x6b, 0x78, 0x70, 1, 0, 0, 0));
    expect(harness.actions).toEqual([
      `mkdir:${coreStateRoot}`, `write:${statePath}`, "start", "F4", "F2", `write:${statePath}`,
    ]);
    expect(harness.runtime).not.toHaveProperty("sendCommand");
    expect(harness.runtime).not.toHaveProperty("saveState");
    expect(harness.runtime).not.toHaveProperty("loadState");
    await mounted.cleanup();
    harness.frame.remove();
  });

  it("follows the current PhysFS evidence directory after restore changes it", async () => {
    const harness = createHarness();
    harness.onKeyDown = (code) => {
      harness.actions.push(code);
      if (code === "F4") {
        harness.files.delete(evidencePath);
        harness.setSaveDirectoryName("RETROM RPGVX-");
        harness.files.set(movedEvidencePath, positionBytes(7, 4, 6, 9, 600));
      }
    };

    const mountPromise = mountMkxp(mkxpConfig(true), harness.target, stateFixture, harness.dependencies);
    await vi.advanceTimersByTimeAsync(1_000);

    const mounted = await mountPromise;
    expect(mounted.position()).toEqual({ mapId: 7, playerX: 4, playerY: 6, fixtureState: 9 });
    const getFrameNum = mounted.instance.gameManager?.getFrameNum;
    if (!getFrameNum) {throw new Error("getFrameNum unavailable");}
    expect(getFrameNum()).toBe(600);
    await mounted.cleanup();
    harness.frame.remove();
  });

  it("does not declare a restore ready when the bridge never reaches saved position B", async () => {
    const harness = createHarness();
    harness.onKeyDown = (code) => {
      harness.actions.push(code);
      if (code === "F4") {harness.files.set(evidencePath, positionBytes(7, 8, 6, 10, 601));}
    };
    const result = mountMkxp(mkxpConfig(true), harness.target, stateFixture, harness.dependencies)
      .then(() => null, (error: unknown) => error);

    await vi.advanceTimersByTimeAsync(31_000);

    await expect(result).resolves.toMatchObject({ message: "RPG_CHECKPOINT_RESTORE_FAILED" });
    expect(harness.actions).toEqual([`mkdir:${coreStateRoot}`, `write:${statePath}`, "start", "F4"]);
    expect(harness.runtime.exit).toHaveBeenCalledOnce();
    expect(harness.target.childElementCount).toBe(0);
    harness.frame.remove();
  });

  it("maps a malformed raw core restore payload to the restore failure stage", async () => {
    const harness = createHarness();
    const invalidState = new Uint8Array(stateSize);

    await expect(mountMkxp(mkxpConfig(true), harness.target, invalidState, harness.dependencies))
      .rejects.toThrow("RPG_CHECKPOINT_RESTORE_FAILED");

    expect(harness.runtime.start).not.toHaveBeenCalled();
    expect(harness.runtime.exit).toHaveBeenCalledOnce();
    expect(harness.target.childElementCount).toBe(0);
    harness.frame.remove();
  });

  it("times out checkpoint creation without calling a direct RetroArch command", async () => {
    const harness = createHarness();
    harness.onKeyDown = (code) => {harness.actions.push(code);};
    const mountPromise = mountMkxp(mkxpConfig(false), harness.target, null, harness.dependencies);
    await vi.advanceTimersByTimeAsync(1_000);
    const mounted = await mountPromise;
    const result = mounted.instance.gameManager?.getStateAsync?.().then(() => null, (error: unknown) => error);

    await vi.advanceTimersByTimeAsync(121_000);

    await expect(result).resolves.toMatchObject({ message: "RPG_CHECKPOINT_CREATE_TIMEOUT" });
    expect(harness.actions).toEqual([`mkdir:${coreStateRoot}`, "start", "F2"]);
    expect(harness.runtime).not.toHaveProperty("sendCommand");
    await mounted.cleanup();
    harness.frame.remove();
  });

  it("pauses and resumes through the core input loop without calling Nostalgist GL commands", async () => {
    const harness = createHarness();
    harness.runtime.pause.mockImplementation(() => {throw new Error("useProgram");});
    harness.runtime.resume.mockImplementation(() => {throw new Error("useProgram");});
    harness.onKeyDown = (code) => {harness.actions.push(code);};
    const mountPromise = mountMkxp(mkxpConfig(false), harness.target, null, harness.dependencies);
    await vi.advanceTimersByTimeAsync(1_000);
    const mounted = await mountPromise;
    const toggle = mounted.instance.gameManager?.toggleMainLoop;
    if (!toggle) {throw new Error("toggleMainLoop unavailable");}

    const pause = toggle(false);
    await vi.advanceTimersByTimeAsync(100);
    await pause;
    expect(mounted.instance.paused).toBe(true);
    const resume = toggle(true);
    await vi.advanceTimersByTimeAsync(100);
    await resume;
    expect(mounted.instance.paused).toBe(false);

    expect(harness.actions).toEqual([`mkdir:${coreStateRoot}`, "start", "F6", "F6"]);
    expect(harness.runtime.pause).not.toHaveBeenCalled();
    expect(harness.runtime.resume).not.toHaveBeenCalled();
    await mounted.cleanup();
    harness.frame.remove();
  });
});

function createHarness() {
  vi.useFakeTimers();
  Object.defineProperty(window, "crossOriginIsolated", { configurable: true, value: true });
  const files = new Map<string, Uint8Array>();
  const actions: string[] = [];
  const directories: string[] = [];
  let saveDirectoryName = "RETROM RPGXP-";
  const fileSystem = {
    analyzePath: (path: string) => ({ exists: files.has(path) }),
    mkdirTree: (path: string) => {
      directories.push(path);
      if (path === coreStateRoot) {actions.push(`mkdir:${path}`);}
    },
    readdir: (path: string) => {
      if (path === "/home/web_user/retroarch/userdata/saves/mkxp-z/mkxp-z/Saves") {
        return [".", "..", saveDirectoryName];
      }
      throw new Error("ENOENT");
    },
    readFile: (path: string) => {
      const contents = files.get(path);
      if (!contents) {throw new Error("ENOENT");}
      return contents;
    },
    stat: (path: string) => {
      const contents = files.get(path);
      if (!contents) {throw new Error("ENOENT");}
      return { size: contents.byteLength };
    },
    unlink: (path: string) => {files.delete(path);},
    rename: (from: string, to: string) => {
      const contents = files.get(from);
      if (!contents) {throw new Error("ENOENT");}
      files.set(to, contents);
      files.delete(from);
    },
    writeFile: (path: string, contents: Uint8Array) => {
      if (path === statePath && !directories.includes(coreStateRoot)) {throw new Error("ENOENT");}
      files.set(path, contents);
      if (path === statePath) {actions.push(`write:${path}`);}
    },
  };
  const harness = {
    actions,
    directories,
    files,
    setSaveDirectoryName: (name: string) => {saveDirectoryName = name;},
    writeRuntimeState: (contents: Uint8Array) => fileSystem.writeFile(statePath, contents),
    onKeyDown: (code: string) => {void code;},
    canvasIdAtPrepare: null as string | null,
    prepareOptions: null as Parameters<typeof Nostalgist.prepare>[0] | null,
    stateAtStart: undefined as Uint8Array | undefined,
    frame: document.createElement("iframe"),
    target: undefined as unknown as HTMLElement,
    runtime: undefined as unknown as ReturnType<typeof runtimeFixture>,
    dependencies: undefined as unknown as Parameters<typeof mountMkxp>[3],
  };
  const runtime = runtimeFixture(fileSystem, () => {
    harness.stateAtStart = files.get(statePath);
    files.set(evidencePath, positionBytes(7, 3, 6, 9, 599));
    actions.push("start");
  });
  harness.runtime = runtime;
  document.body.append(harness.frame);
  const target = harness.frame.contentDocument?.createElement("div");
  if (!target || !harness.frame.contentDocument) {throw new Error("test frame unavailable");}
  harness.frame.contentDocument.body.append(target);
  harness.target = target;
  harness.dependencies = {
    fetchVerified: async () => Uint8Array.of(1),
    prepare: async (options) => {
      harness.prepareOptions = options;
      if (!(options.element instanceof HTMLCanvasElement)) {throw new TypeError("invalid element");}
      harness.canvasIdAtPrepare = options.element.id;
      options.element.id = "canvas";
      options.element.addEventListener("keydown", (event) => harness.onKeyDown(event.code));
      return runtime;
    },
  };
  return harness;
}

function runtimeFixture(fileSystem: TestFileSystem, onStart: () => void) {
  return {
    exit: vi.fn(async () => undefined),
    getEmscriptenFS: () => fileSystem,
    pause: vi.fn(),
    resume: vi.fn(),
    start: vi.fn(async () => {onStart();}),
  };
}

function positionBytes(mapId: number, playerX: number, playerY: number, fixtureState: number, frameCount: number) {
  return new TextEncoder().encode(`1,${mapId},${playerX},${playerY},${fixtureState},${frameCount}`);
}

function mkxpConfig(restore: boolean): MkxpConfig {
  const launchId = "01980000-0000-7000-8000-000000000001";
  const runtimeValidation = restore ? {
    validationId: "01980000-0000-7000-8000-000000000003",
    state: "CHECKPOINTED" as const,
    originalLaunchId: "01980000-0000-7000-8000-000000000004",
    restoreLaunchId: launchId,
    lastGateSequence: 20,
    machineGates: rpgValidationGates.map((gate) => ({
      gate,
      status: gate === "SAVE_POINT_RECORDED" ? "PASSED" as const : "NOT_STARTED" as const,
      begunAtMs: gate === "SAVE_POINT_RECORDED" ? 1 : null,
      completedAtMs: gate === "SAVE_POINT_RECORDED" ? 2 : null,
      evidence: gate === "SAVE_POINT_RECORDED" ? { mapId: 7, playerX: 4, playerY: 6, fixtureState: 9 } : null,
      failureCode: null,
    })),
    checkpointEvidence: null,
    restoreScreenshotUploaded: false,
  } : null;
  return {
    runtimeFamily: "RPGMAKER",
    protocolVersion: 1,
    mode: "single",
    purpose: restore ? "RPG_RUNTIME_VALIDATION" : "PRODUCT",
    launchId,
    coreId: "rpgmaker_xp",
    coreName: "RPG Maker XP",
    gameTitle: "Validation fixture",
    platformName: "RPG Maker",
    returnTo: "/admin/reviews/item",
    warnings: [],
    generation: "RPGXP",
    routeKey: "RPGXP_MKXPZ_F2EFC98_V5",
    artifactId: "01980000-0000-7000-8000-000000000002",
    checkpoint: restore ? { payloadKind: "RUNTIME_STATE", payloadUrl: `/runtime/launches/${launchId}/state` } : null,
    checkpointAvailability: { available: true, reason: null },
    runtimeValidation,
    adapter: {
      adapterKind: "MKXP_LIBRETRO_WEB",
      adapterId: "mkxp-z-libretro-v4",
      runtimeBaseUrl: "/runtime/rpgmaker/f2efc98-v5/",
      core: {
        jsUrl: "/runtime/rpgmaker/f2efc98-v5/mkxp-z_libretro.js",
        jsSizeBytes: 258192,
        jsSha256: "c".repeat(64),
        wasmUrl: "/runtime/rpgmaker/f2efc98-v5/mkxp-z_libretro.wasm",
        wasmSizeBytes: 42487229,
        wasmSha256: "d".repeat(64),
        artifactSetSha256: "a".repeat(64),
      },
      projectArchive: {
        url: `/runtime/rpg-project/${launchId}/__retrom__/game.mkxpz`,
        sha256: "b".repeat(64),
        sizeBytes: 1,
      },
      rtpArchives: [],
      rgssVersion: 1,
      stateBufferBytes: stateSize,
    },
  };
}
