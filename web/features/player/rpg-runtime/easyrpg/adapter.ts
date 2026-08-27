import { unzip } from "fflate";
import type { RpgPlayerInstance } from "../internal-adapter";
import { decodeRpgCheckpoint, encodeRpgCheckpoint } from "../checkpoint";
import type { RpgPosition, RpgRuntimeConfig } from "../contract";

type EasyConfig = RpgRuntimeConfig & { adapter: Extract<RpgRuntimeConfig["adapter"], { adapterKind: "EASYRPG_WEB" }> };

type EasyFileSystem = {
  analyzePath(path: string): { exists: boolean };
  readFile(path: string): Uint8Array;
};

type EasyState = RpgPosition & {
  engine: "RPG2000" | "RPG2003";
  ready: boolean;
  canCheckpoint: boolean;
  frameCount: number;
};

type EasyModule = {
  FS: EasyFileSystem;
  api: {
    createRetromCheckpoint(): boolean;
    retromState(): string;
  };
  canvas: HTMLCanvasElement;
  retromFileSystemReady?: boolean;
  initApi(): void;
  pauseMainLoop(): void;
  resumeMainLoop(): void;
};

type EasyModuleOptions = {
  game: string;
  noExitRuntime: true;
  saveFs: undefined;
  locateFile(path: string): string;
  retromEngineMode: string;
  retromRtpMountPath?: string;
  retromRtpFiles: Array<{ path: string; bytes: Uint8Array }>;
  retromRestoreSlot?: number;
  retromRestoreFiles: Array<{ path: string; bytes: Uint8Array }>;
};

type EasyWindow = Window & {
  createEasyRpgPlayer?: (options: EasyModuleOptions) => Promise<EasyModule>;
};

const maximumArchiveBytes = 512 * 1024 * 1024;
const savePath = "Save/Save100.lsd";

export async function mountEasyRpg(
  config: EasyConfig,
  target: HTMLElement,
  frameWindow: Window,
  restorePayload: Uint8Array | null,
) {
  try {return await mountEasyRpgUnchecked(config, target, frameWindow, restorePayload);}
  catch (error) {
    target.replaceChildren();
    frameWindow.document.querySelectorAll("script[data-retrom-rpg-runtime]").forEach((script) => script.remove());
    throw error;
  }
}

async function mountEasyRpgUnchecked(
  config: EasyConfig,
  target: HTMLElement,
  frameWindow: Window,
  restorePayload: Uint8Array | null,
) {
  const runtimeWindow = frameWindow as EasyWindow;
  const document = frameWindow.document;
  const canvas = document.createElement("canvas");
  canvas.id = "canvas";
  canvas.tabIndex = 0;
  retainDisplayedWebGLFrame(canvas);
  const status = document.createElement("div");
  status.id = "status";
  target.append(canvas, status);

  const [rtpFiles, restoreFiles] = await Promise.all([
    loadRtp(config),
    decodeRestore(config, restorePayload),
  ]);
  const script = await loadScript(document, `${config.adapter.runtimeBaseUrl}easyrpg-player.js`);
  const createPlayer = runtimeWindow.createEasyRpgPlayer;
  if (typeof createPlayer !== "function") {
    script.remove();
    throw new Error("RPG_RUNTIME_ARTIFACT_INVALID");
  }
  const playerModule = await createPlayer({
    game: config.launchId,
    noExitRuntime: true,
    saveFs: undefined,
    locateFile: (path) => `${config.adapter.runtimeBaseUrl}${path}`,
    retromEngineMode: config.adapter.engineMode,
    ...(config.adapter.rtpArchive ? { retromRtpMountPath: config.adapter.rtpArchive.mountPath } : {}),
    retromRtpFiles: rtpFiles,
    ...(restoreFiles.length ? { retromRestoreSlot: config.adapter.checkpointSlot } : {}),
    retromRestoreFiles: restoreFiles,
  });
  playerModule.initApi();
  const expectedEngine = config.generation === "RPG2000" ? "RPG2000" : "RPG2003";
  await waitForReady(playerModule, expectedEngine);

  const listeners = new Map<string, Set<(...args: unknown[]) => void>>();
  const instance: RpgPlayerInstance = {
    canvas: playerModule.canvas,
    paused: false,
    volume: 1,
    muted: false,
    on(event, callback) {
      const current = listeners.get(event) ?? new Set();
      current.add(callback);
      listeners.set(event, current);
    },
    takeScreenshot: async () => ({ blob: await canvasBlob(playerModule.canvas), format: "png" }),
    gameManager: {
      savePayloadKind: "NATIVE_SAVE_BUNDLE_V1",
      validationPurpose: config.purpose === "RPG_RUNTIME_VALIDATION",
      getRpgPosition: () => position(readState(playerModule)),
      getCheckpointAvailability: () => {
        const available = readState(playerModule).canCheckpoint;
        return { available, reason: available ? null : "BUSY" };
      },
      getStateAsync: async () => {
        if (!playerModule.api.createRetromCheckpoint()) {throw new Error("RPG_CHECKPOINT_UNAVAILABLE");}
        const bytes = readCheckpoint(playerModule.FS);
        return encodeRpgCheckpoint({
          engine: expectedEngine,
          resumeSlot: config.adapter.checkpointSlot,
          entries: [{ store: "FILESYSTEM", key: savePath, mediaType: "application/octet-stream", data: bytes }],
        });
      },
      getFrameNum: () => readState(playerModule).frameCount,
      getVideoDimensions: (dimension) => dimension === "aspect" ? playerModule.canvas.width / playerModule.canvas.height :
        dimension === "width" ? playerModule.canvas.width : playerModule.canvas.height,
      toggleMainLoop: (running) => {
        if (running) {playerModule.resumeMainLoop(); instance.paused = false;}
        else {playerModule.pauseMainLoop(); instance.paused = true;}
      },
    },
  };
  return {
    instance,
    position: () => position(readState(playerModule)),
    checkpointAvailable: () => readState(playerModule).canCheckpoint,
    cleanup: () => {
      playerModule.pauseMainLoop();
      script.remove();
      target.replaceChildren();
      listeners.clear();
    },
  };
}

function readState(module: EasyModule): EasyState {
  let parsed: unknown;
  try {parsed = JSON.parse(module.api.retromState());}
  catch {throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");}
  if (!parsed || typeof parsed !== "object") {throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");}
  const state = parsed as Partial<EasyState>;
  if ((state.engine !== "RPG2000" && state.engine !== "RPG2003") || typeof state.ready !== "boolean" ||
    typeof state.canCheckpoint !== "boolean" || !validInteger(state.frameCount, 0) || !validPosition(state)) {
    throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");
  }
  return state as EasyState;
}

function validPosition(value: Partial<RpgPosition>) {
  return validInteger(value.mapId, 0) && validInteger(value.playerX) && validInteger(value.playerY) &&
    validInteger(value.fixtureState);
}

function validInteger(value: unknown, minimum = -2_147_483_648) {
  return Number.isSafeInteger(value) && Number(value) >= minimum && Number(value) <= 2_147_483_647;
}

function position(state: EasyState): RpgPosition {
  return { mapId: state.mapId, playerX: state.playerX, playerY: state.playerY, fixtureState: state.fixtureState };
}

async function waitForReady(
  module: EasyModule,
  expectedEngine: EasyState["engine"],
) {
  if (module.retromFileSystemReady !== true) {
    throw new Error("RPG_RUNTIME_FILESYSTEM_NOT_READY");
  }
  const deadline = performance.now() + 30_000;
  while (performance.now() < deadline) {
    const state = startupState(module);
    if (state?.ready) {
      if (state.engine !== expectedEngine) {throw new Error("RPG_ENGINE_PROFILE_MISMATCH");}
      return;
    }
    await new Promise<void>((resolve) => window.setTimeout(resolve, 50));
  }
  throw new Error("RPG_RUNTIME_TIMEOUT");
}

function startupState(module: EasyModule) {
  try {return readState(module);}
  catch (error) {
    if (error instanceof Error && error.message === "RPG_RUNTIME_POSITION_UNAVAILABLE") {return null;}
    throw error;
  }
}

async function decodeRestore(config: EasyConfig, payload: Uint8Array | null) {
  if (!payload) {return [];}
  const expectedEngine = config.generation === "RPG2000" ? "RPG2000" : "RPG2003";
  const bundle = await decodeRpgCheckpoint(payload, expectedEngine);
  if (bundle.resumeSlot !== config.adapter.checkpointSlot || bundle.entries.length !== 1) {
    throw new Error("RPG_CHECKPOINT_RESTORE_INVALID");
  }
  const entry = bundle.entries[0];
  if (entry.store !== "FILESYSTEM" || entry.key !== savePath) {throw new Error("RPG_CHECKPOINT_RESTORE_INVALID");}
  return [{ path: entry.key, bytes: entry.data }];
}

function readCheckpoint(fileSystem: EasyFileSystem) {
  const candidates = [savePath, `/${savePath}`];
  for (const candidate of candidates) {
    if (!fileSystem.analyzePath(candidate).exists) {continue;}
    const value = fileSystem.readFile(candidate);
    if (value.byteLength) {return new Uint8Array(value).slice();}
  }
  throw new Error("RPG_CHECKPOINT_CREATE_FAILED");
}

async function loadRtp(config: EasyConfig) {
  const archive = config.adapter.rtpArchive;
  if (!archive) {return [];}
  const bytes = await fetchBytes(archive.url, maximumArchiveBytes);
  if (await digest(bytes) !== archive.sha256) {throw new Error("RPG_RUNTIME_PACK_DIGEST_MISMATCH");}
  const files = await unzipBytes(bytes);
  let total = 0;
  const result: Array<{ path: string; bytes: Uint8Array }> = [];
  const names = Object.keys(files).sort();
  if (names.length > 10_000) {throw new Error("RPG_RUNTIME_PACK_INVALID");}
  for (const name of names) {
    if (!safeArchivePath(name) || name.endsWith("/")) {continue;}
    const contents = files[name];
    total += contents.byteLength;
    if (total > maximumArchiveBytes) {throw new Error("RPG_RUNTIME_PACK_INVALID");}
    result.push({ path: `${archive.mountPath}/${name}`, bytes: contents });
  }
  if (!result.length) {throw new Error("RPG_RUNTIME_PACK_INVALID");}
  return result;
}

function safeArchivePath(value: string) {
  return Boolean(value) && value.length <= 512 && !value.startsWith("/") && !value.includes("\\") &&
    value.split("/").every((part) => part !== "" && part !== "." && part !== "..");
}

function unzipBytes(bytes: Uint8Array) {
  return new Promise<Record<string, Uint8Array>>((resolve, reject) => {
    unzip(bytes, (error, files) => error ? reject(new Error("RPG_RUNTIME_PACK_INVALID")) : resolve(files));
  });
}

async function fetchBytes(url: string, maximum: number) {
  const response = await fetch(url, { credentials: "same-origin", cache: "no-store", redirect: "error" });
  if (!response.ok || response.url !== new URL(url, window.location.href).href) {throw new Error("RPG_RUNTIME_CONTENT_UNAVAILABLE");}
  const bytes = new Uint8Array(await response.arrayBuffer());
  if (!bytes.byteLength || bytes.byteLength > maximum) {throw new Error("RPG_RUNTIME_CONTENT_UNAVAILABLE");}
  return bytes;
}

async function digest(bytes: Uint8Array) {
  const value = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes.slice().buffer));
  return [...value].map((item) => item.toString(16).padStart(2, "0")).join("");
}

function loadScript(document: Document, url: string) {
  return new Promise<HTMLScriptElement>((resolve, reject) => {
    const script = document.createElement("script");
    script.dataset.retromRpgRuntime = "easyrpg";
    script.src = url;
    script.async = true;
    script.addEventListener("load", () => resolve(script), { once: true });
    script.addEventListener("error", () => {
      script.remove();
      reject(new Error("RPG_RUNTIME_ARTIFACT_UNAVAILABLE"));
    }, { once: true });
    document.head.append(script);
  });
}

async function canvasBlob(canvas: HTMLCanvasElement) {
  const deadline = performance.now() + 10_000;
  while (performance.now() < deadline) {
    const blob = await encodeCanvas(canvas);
    if (await visibleEncodedFrame(canvas, blob)) {return blob;}
    await new Promise<void>((resolve) => setTimeout(resolve, 100));
  }
  throw new Error("PLAYER_SCREENSHOT_UNAVAILABLE");
}

function encodeCanvas(canvas: HTMLCanvasElement) {
  return new Promise<Blob>((resolve, reject) => canvas.toBlob((blob) => {
    if (blob?.size) {resolve(blob);} else {reject(new Error("PLAYER_SCREENSHOT_UNAVAILABLE"));}
  }, "image/png"));
}

async function visibleEncodedFrame(canvas: HTMLCanvasElement, blob: Blob) {
  const frameWindow = canvas.ownerDocument.defaultView;
  if (!frameWindow?.createImageBitmap) {throw new Error("PLAYER_SCREENSHOT_UNAVAILABLE");}
  const bitmap = await frameWindow.createImageBitmap(blob);
  const width = Math.min(bitmap.width, 160);
  const height = Math.min(bitmap.height, Math.max(1, Math.round(bitmap.height * width / bitmap.width)));
  const probe = canvas.ownerDocument.createElement("canvas");
  probe.width = width;
  probe.height = height;
  const context = probe.getContext("2d", { willReadFrequently: true });
  if (!context) {bitmap.close(); throw new Error("PLAYER_SCREENSHOT_UNAVAILABLE");}
  context.drawImage(bitmap, 0, 0, width, height);
  bitmap.close();
  const pixels = context.getImageData(0, 0, width, height).data;
  let visible = 0;
  const minimum = Math.max(16, Math.floor(width * height / 200));
  for (let offset = 0; offset < pixels.length; offset += 4) {
    if (Math.max(pixels[offset], pixels[offset + 1], pixels[offset + 2]) >= 16) {
      visible += 1;
      if (visible >= minimum) {return true;}
    }
  }
  return false;
}

function retainDisplayedWebGLFrame(canvas: HTMLCanvasElement) {
  const getContext = canvas.getContext;
  Object.defineProperty(canvas, "getContext", {
    configurable: true,
    value(contextId: string, options?: unknown) {
      const webGL = contextId === "webgl" || contextId === "webgl2" || contextId === "experimental-webgl";
      const attributes = options && typeof options === "object" ? options : {};
      const effectiveOptions = webGL ? { ...attributes, preserveDrawingBuffer: true } : options;
      return Reflect.apply(getContext, canvas, [contextId, effectiveOptions]);
    },
  });
}
