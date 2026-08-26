import { Nostalgist } from "nostalgist";
import type { RpgPlayerInstance } from "../internal-adapter";
import type { RpgPosition, RpgRuntimeConfig } from "../contract";
import { decodeMkxpRastate, encodeMkxpRastate, mkxpRastateEnvelopeBytes } from "./state";

type MkxpConfig = RpgRuntimeConfig & { adapter: Extract<RpgRuntimeConfig["adapter"], { adapterKind: "MKXP_LIBRETRO_WEB" }> };

type MkxpFileSystem = {
  analyzePath(path: string): { exists: boolean };
  mkdirTree(path: string): void;
  readdir(path: string): string[];
  readFile(path: string): Uint8Array;
  rename(from: string, to: string): void;
  stat(path: string): { size: number };
  unlink(path: string): void;
  writeFile(path: string, contents: Uint8Array): void;
};

type MkxpRuntime = Pick<Nostalgist,
  "exit" | "getEmscriptenFS" | "start"
>;

type MkxpMountDependencies = {
  fetchVerified: (url: string, expectedSize: number, expectedDigest: string) => Promise<Uint8Array>;
  prepare: (options: Parameters<typeof Nostalgist.prepare>[0]) => Promise<MkxpRuntime>;
};

const bridgeV3 = { size: 1487, sha256: "524a0b3210f33f5a01220134548eb2947678ca5ef7a75b5caf3cfea2804fd5fa" };
const systemRoot = "/home/web_user/retroarch/userdata/system";
const saveRoot = "/home/web_user/retroarch/userdata/saves";
const stateRoot = "/home/web_user/retroarch/userdata/states";
const coreStateRoot = `${stateRoot}/mkxp-z`;
const statePath = `${coreStateRoot}/game.state`;
const bridgePath = `${systemRoot}/mkxp-z/Scripts/Preload/retrom_position.rb`;
const evidenceName = "retrom-position-v1";
const bridgeOption = "mkxp-z_preload-726574726f6d5f706f736974696f6e2e7262";
const saveStateHotkey = { code: "F2", keyCode: 113 } as const;
const loadStateHotkey = { code: "F4", keyCode: 115 } as const;
const pauseToggleHotkey = { code: "F6", keyCode: 117 } as const;
const browserDependencies: MkxpMountDependencies = {
  fetchVerified,
  prepare: (options) => Nostalgist.prepare(options),
};

export async function mountMkxp(
  config: MkxpConfig,
  target: HTMLElement,
  restorePayload: Uint8Array | null,
  dependencies: MkxpMountDependencies = browserDependencies,
) {
  try {return await mountMkxpUnchecked(config, target, restorePayload, dependencies);}
  catch (error) {target.replaceChildren(); throw error;}
}

async function mountMkxpUnchecked(
  config: MkxpConfig,
  target: HTMLElement,
  restorePayload: Uint8Array | null,
  dependencies: MkxpMountDependencies,
) {
  if (!window.crossOriginIsolated || typeof SharedArrayBuffer === "undefined") {
    throw new Error("RPG_RUNTIME_THREADS_REQUIRED");
  }
  // Nostalgist validates the element against the HTMLCanvasElement constructor
  // in its own module realm. The Player target lives in a same-origin child
  // document, so creating the canvas through target.ownerDocument makes a
  // genuine canvas fail that cross-realm instanceof check. Create it in the
  // adapter realm and let append() adopt it into the Player frame.
  const canvas = document.createElement("canvas");
  // Nostalgist assigns this exact ID during prepare. Use it from the first DOM
  // insertion so the Player never exposes a transient selector identity.
  canvas.id = "canvas";
  canvas.tabIndex = 0;
  target.append(canvas);
  const [jsBytes, wasmBytes, gameBytes, bridgeBytes, ...rtpBytes] = await Promise.all([
    dependencies.fetchVerified(
      config.adapter.core.jsUrl, config.adapter.core.jsSizeBytes, config.adapter.core.jsSha256,
    ),
    dependencies.fetchVerified(
      config.adapter.core.wasmUrl, config.adapter.core.wasmSizeBytes, config.adapter.core.wasmSha256,
    ),
    dependencies.fetchVerified(config.adapter.projectArchive.url, config.adapter.projectArchive.sizeBytes, config.adapter.projectArchive.sha256),
    dependencies.fetchVerified(`${config.adapter.runtimeBaseUrl}retrom_position.rb`, bridgeV3.size, bridgeV3.sha256),
    ...config.adapter.rtpArchives.map((archive) => dependencies.fetchVerified(archive.url, archive.sizeBytes, archive.sha256)),
  ]);
  const nostalgist = await dependencies.prepare({
    core: {
      name: "mkxp-z",
      js: new Blob([jsBytes.slice().buffer], { type: "text/javascript" }),
      wasm: new Blob([wasmBytes.slice().buffer], { type: "application/wasm" }),
    },
    rom: { fileName: "game.mkxpz", fileContent: new Blob([gameBytes.slice().buffer]) },
    bios: config.adapter.rtpArchives.map((archive, index) => ({
      fileName: runtimePackFileName(index, archive.declaredName),
      fileContent: new Blob([rtpBytes[index].slice().buffer]),
    })),
    element: canvas,
    retroarchConfig: {
      savefile_directory: "/home/web_user/retroarch/userdata/saves",
      savestate_directory: "/home/web_user/retroarch/userdata/states",
      system_directory: systemRoot,
      input_menu_toggle: "nul",
      input_save_state: "f2",
      input_load_state: "f4",
      input_pause_toggle: "f6",
      // Retrom persists the exact raw core payload and validates an exact
      // RASTATE1 runtime envelope. RetroArch otherwise defaults to rzip for
      // savestates, which changes the file format and defers file visibility.
      savestate_file_compression: false,
      savestate_thumbnail_enable: false,
      log_verbosity: true,
    },
    retroarchCoreConfig: {
      "mkxp-z_rgssVersion": String(config.adapter.rgssVersion),
      "mkxp-z_saveStateSize": String(config.adapter.stateBufferBytes / (1024 * 1024)),
      [bridgeOption]: "enabled",
    },
  });
  const fileSystem = nostalgist.getEmscriptenFS() as MkxpFileSystem;
  try {
    installRuntimeFiles(fileSystem, config, bridgeBytes, rtpBytes);
    if (restorePayload) {installRestoreState(fileSystem, restorePayload, config.adapter.stateBufferBytes);}
    await nostalgist.start();
  } catch (error) {
    await nostalgist.exit();
    throw error;
  }
  let evidencePath: string;
  try {
    evidencePath = await prepareEvidencePath(fileSystem);
    if (restorePayload) {
      await restoreStateAndWait(canvas, fileSystem, evidencePath, expectedRestorePosition(config));
    }
  } catch (error) {
    await nostalgist.exit();
    throw error;
  }
  const expectedProfile = `rgss${config.adapter.rgssVersion}`;

  const listeners = new Map<string, Set<(...args: unknown[]) => void>>();
  const instance: RpgPlayerInstance = {
    canvas,
    paused: false,
    volume: 1,
    muted: false,
    on(event, callback) {
      const current = listeners.get(event) ?? new Set();
      current.add(callback);
      listeners.set(event, current);
    },
    // Nostalgist's screenshot command calls RetroArch's exported GL function
    // from the browser main thread. The mkxp core owns its WebGL context on a
    // pthread, so that call has no GLctx and crashes in useProgram. Capturing
    // the displayed canvas stays on the browser side of that thread boundary.
    takeScreenshot: async () => ({ blob: await canvasBlob(canvas), format: "png" }),
    gameManager: {
      savePayloadKind: "RUNTIME_STATE",
      validationPurpose: config.purpose === "RPG_RUNTIME_VALIDATION",
      getRpgPosition: () => readPosition(fileSystem, evidencePath).position,
      getCheckpointAvailability: () => ({ available: true, reason: null }),
      getStateAsync: () => saveStateBytes(canvas, fileSystem, config.adapter.stateBufferBytes),
      getFrameNum: () => readPosition(fileSystem, evidencePath).frameCount,
      getVideoDimensions: (dimension) => dimension === "aspect" ? canvas.width / canvas.height :
        dimension === "width" ? canvas.width : canvas.height,
      toggleMainLoop: async (running) => {
        // Nostalgist pause/resume calls RetroArch's exported command from the
        // browser main thread. The mkxp WebGL context belongs to a pthread, so
        // that path crashes in glUseProgram. Keep the command in RetroArch's
        // normal input/render loop, as for save and restore.
        await pressPrivateHotkey(canvas, pauseToggleHotkey);
        instance.paused = !running;
      },
    },
  };
  return {
    instance,
    engineProfile: expectedProfile,
    position: () => readPosition(fileSystem, evidencePath).position,
    cleanup: async () => {
      await nostalgist.exit();
      target.replaceChildren();
      listeners.clear();
    },
  };
}

function installRuntimeFiles(
  fileSystem: MkxpFileSystem,
  config: MkxpConfig,
  bridgeBytes: Uint8Array,
  rtpBytes: Uint8Array[],
) {
  fileSystem.mkdirTree(`${systemRoot}/mkxp-z/Scripts/Preload`);
  fileSystem.mkdirTree(saveRoot);
  // Nostalgist only creates the per-core state directory when its `state`
  // option is present. Retrom cannot use that option because custom mkxp-z is
  // absent from Nostalgist's core map, so own the exact directory here.
  fileSystem.mkdirTree(coreStateRoot);
  fileSystem.writeFile(bridgePath, bridgeBytes);
  if (!fileSystem.analyzePath(bridgePath).exists) {throw new Error("RPG_RUNTIME_BRIDGE_UNAVAILABLE");}
  const rtpRoot = `${systemRoot}/mkxp-z/RTP`;
  fileSystem.mkdirTree(rtpRoot);
  for (const [index, archive] of config.adapter.rtpArchives.entries()) {
    const name = runtimePackFileName(index, archive.declaredName);
    const source = `${systemRoot}/${name}`;
    const destination = `${rtpRoot}/${name}`;
    if (!fileSystem.analyzePath(source).exists || rtpBytes[index].byteLength !== archive.sizeBytes) {
      throw new Error("RPG_RUNTIME_PACK_INVALID");
    }
    fileSystem.rename(source, destination);
  }
}

async function prepareEvidencePath(fileSystem: MkxpFileSystem) {
  // RetroArch first scopes savefile_directory by the core name and mkxp-z then
  // mounts its own mkxp-z/Saves directory below that effective save root.
  const gameSaveRoot = `${saveRoot}/mkxp-z/mkxp-z/Saves`;
  const deadline = performance.now() + 30_000;
  while (performance.now() < deadline) {
    const names = readDirectory(fileSystem, gameSaveRoot);
    if (names.length > 1) {throw new Error("RPG_RUNTIME_BRIDGE_UNAVAILABLE");}
    if (names.length === 1) {
      const directory = `${gameSaveRoot}/${names[0]}`;
      const evidencePath = `${directory}/${evidenceName}`;
      if (fileSystem.analyzePath(evidencePath).exists) {return evidencePath;}
    }
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error("RPG_RUNTIME_BRIDGE_UNAVAILABLE");
}

function readDirectory(fileSystem: MkxpFileSystem, path: string) {
  try {
    return fileSystem.readdir(path).filter((name) => name !== "." && name !== "..");
  } catch {
    return [];
  }
}

function runtimePackFileName(index: number, declaredName: string) {
  if (index < 0 || index > 2 || !declaredName || declaredName.length > 240 ||
    declaredName.includes("/") || declaredName.includes("\\") || /[\u0000-\u001f\u007f]/u.test(declaredName) ||
    declaredName === "." || declaredName === "..") {
    throw new Error("RPG_RUNTIME_PACK_INVALID");
  }
  const suffix = declaredName.toLowerCase().endsWith(".mkxpz") ? "" : ".mkxpz";
  return `${declaredName}${suffix}`;
}

function readPosition(fileSystem: MkxpFileSystem, evidencePath: string) {
  if (!fileSystem.analyzePath(evidencePath).exists) {throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");}
  const text = new TextDecoder("utf-8", { fatal: true }).decode(fileSystem.readFile(evidencePath));
  const parts = text.split(",");
  if (parts.length !== 6 || parts[0] !== "1") {throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");}
  const values = parts.slice(1).map((value) => Number(value));
  if (values.some((value) => !Number.isSafeInteger(value) || value < -2_147_483_648 || value > 2_147_483_647) || values[0] < 0 || values[4] < 0) {
    throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");
  }
  return {
    position: { mapId: values[0], playerX: values[1], playerY: values[2], fixtureState: values[3] } satisfies RpgPosition,
    frameCount: values[4],
  };
}

async function fetchVerified(url: string, expectedSize: number, expectedDigest: string) {
  const response = await fetch(url, { credentials: "same-origin", cache: "no-store", redirect: "error" });
  if (!response.ok || response.url !== new URL(url, window.location.href).href) {throw new Error("RPG_RUNTIME_CONTENT_UNAVAILABLE");}
  const bytes = new Uint8Array(await response.arrayBuffer());
  if (bytes.byteLength !== expectedSize || await digest(bytes) !== expectedDigest) {
    throw new Error("RPG_RUNTIME_CONTENT_DIGEST_MISMATCH");
  }
  return bytes;
}

async function digest(bytes: Uint8Array) {
  const result = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes.slice().buffer));
  return [...result].map((value) => value.toString(16).padStart(2, "0")).join("");
}

function canvasBlob(canvas: HTMLCanvasElement) {
  return new Promise<Blob>((resolve, reject) => canvas.toBlob((blob) => {
    if (blob) {resolve(blob);}
    else {reject(new Error("PLAYER_SCREENSHOT_UNAVAILABLE"));}
  }, "image/png"));
}

async function saveStateBytes(canvas: HTMLCanvasElement, fileSystem: MkxpFileSystem, expectedSize: number) {
  try {fileSystem.unlink(statePath);} catch { /* A previous state file is optional. */ }
  await pressPrivateHotkey(canvas, saveStateHotkey);
  const expectedPayloadSize = expectedSize + mkxpRastateEnvelopeBytes;
  const deadline = performance.now() + 120_000;
  while (performance.now() < deadline) {
    try {
      const size = fileSystem.stat(statePath).size;
      if (size > expectedPayloadSize) {throw new Error("RPG_CHECKPOINT_CREATE_FAILED");}
      if (size === expectedPayloadSize) {
        const state = fileSystem.readFile(statePath);
        const core = decodeMkxpRastate(state, expectedSize);
        try {fileSystem.unlink(statePath);} catch { /* The in-memory copy is authoritative. */ }
        return core;
      }
    } catch (error) {
      if (error instanceof Error && error.message === "RPG_CHECKPOINT_CREATE_FAILED") {throw error;}
      // RetroArch creates the file asynchronously after completing core serialization.
    }
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error("RPG_CHECKPOINT_CREATE_TIMEOUT");
}

function installRestoreState(fileSystem: MkxpFileSystem, state: Uint8Array, expectedSize: number) {
  writeState(fileSystem, statePath, state, expectedSize);
}

async function restoreStateAndWait(
  canvas: HTMLCanvasElement,
  fileSystem: MkxpFileSystem,
  evidencePath: string,
  expected: RpgPosition | null,
) {
  const before = tryReadPosition(fileSystem, evidencePath);
  await pressPrivateHotkey(canvas, loadStateHotkey);
  const deadline = performance.now() + 30_000;
  while (performance.now() < deadline) {
    const restored = tryReadPosition(fileSystem, evidencePath);
    if (restored && restored.frameCount !== before?.frameCount &&
      (!expected || samePosition(restored.position, expected))) {
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error("RPG_CHECKPOINT_RESTORE_FAILED");
}

function expectedRestorePosition(config: MkxpConfig): RpgPosition | null {
  if (config.purpose !== "RPG_RUNTIME_VALIDATION") {return null;}
  const evidence = config.runtimeValidation?.machineGates.find((gate) => gate.gate === "SAVE_POINT_RECORDED")?.evidence;
  if (!evidence || !("mapId" in evidence) || !("playerX" in evidence) ||
    !("playerY" in evidence) || !("fixtureState" in evidence)) {
    throw new Error("RPG_RUNTIME_PROTOCOL_VIOLATION");
  }
  return {
    mapId: evidence.mapId,
    playerX: evidence.playerX,
    playerY: evidence.playerY,
    fixtureState: evidence.fixtureState,
  };
}

function tryReadPosition(fileSystem: MkxpFileSystem, evidencePath: string) {
  try {return readPosition(fileSystem, evidencePath);}
  catch (error) {
    if (error instanceof Error && error.message === "RPG_RUNTIME_POSITION_UNAVAILABLE") {return null;}
    throw error;
  }
}

function samePosition(left: RpgPosition, right: RpgPosition) {
  return left.mapId === right.mapId && left.playerX === right.playerX &&
    left.playerY === right.playerY && left.fixtureState === right.fixtureState;
}

async function pressPrivateHotkey(
  canvas: HTMLCanvasElement,
  hotkey: typeof saveStateHotkey | typeof loadStateHotkey | typeof pauseToggleHotkey,
) {
  const KeyboardEventConstructor = canvas.ownerDocument.defaultView?.KeyboardEvent;
  if (!KeyboardEventConstructor) {throw new Error("RPG_RUNTIME_FAILED");}
  const options = {
    bubbles: true,
    cancelable: true,
    code: hotkey.code,
    key: hotkey.code,
    keyCode: hotkey.keyCode,
    which: hotkey.keyCode,
  };
  canvas.focus();
  canvas.dispatchEvent(new KeyboardEventConstructor("keydown", options));
  await new Promise((resolve) => setTimeout(resolve, 100));
  canvas.dispatchEvent(new KeyboardEventConstructor("keyup", options));
}

function writeState(fileSystem: MkxpFileSystem, path: string, state: Uint8Array, expectedSize: number) {
  let rastate: Uint8Array;
  try {rastate = encodeMkxpRastate(state, expectedSize);}
  catch {throw new Error("RPG_CHECKPOINT_RESTORE_FAILED");}
  fileSystem.writeFile(path, rastate);
  if (!fileSystem.analyzePath(path).exists || fileSystem.stat(path).size !== expectedSize + mkxpRastateEnvelopeBytes) {
    throw new Error("RPG_CHECKPOINT_RESTORE_FAILED");
  }
}
