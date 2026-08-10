export type PlayerConfig = {
  launchId: string;
  emulatorjsVersion: string;
  playerAdapterId: string;
  core: string;
  runtimeCore: string;
  coreName: string;
  emulatorGameId: number;
  gameName: string;
  gameTitle: string;
  platformName: string;
  runtimeBaseUrl: string;
  loaderUrl: string;
  gameUrl: string;
  biosUrl: string | null;
  parentUrl: string | null;
  stateUrl: string | null;
  persistentSaveMode: "SINGLE_FILE" | "DOS_OVERLAY" | "NONE";
  persistentSaveUrl: string | null;
  inputMode: "STANDARD" | "POINTER";
  startupActions: StartupAction[];
  requiresThreads: boolean;
  runtimePathOverrides: Record<string, string>;
  defaultCoreOptions: Record<string, string>;
  externalFiles: Record<string, string>;
  discSet?: DiscSet | null;
  dosEntry?: string | null;
  warnings?: string[];
  returnTo: string;
};

export type DiscSet = {
  contentKind: "MULTI_DISC_M3U_V1";
  count: number;
  initialDiscIndex: number;
  entries: DiscSetEntry[];
};

export type DiscSetEntry = {
  index: number;
  label: string;
  virtualPath: string;
};

export type StartupAction = {
  event: "GAME_START";
  kind: "PRESS_CONTROL";
  delayMs: number;
  player: number;
  control: number;
  durationMs: number;
};

export type EmulatorInstance = {
  allSettings?: Record<string, unknown>;
  paused?: boolean;
  volume?: number;
  muted?: boolean;
  setVolume?: (volume: number) => void;
  menu?: { close?: () => void; open?: (force?: boolean) => void; toggle?: () => void };
  controlMenu?: HTMLElement;
  settingsMenu?: HTMLElement;
  settingsMenuOpen?: boolean;
  closeSettingsMenu?: () => void;
  on: (event: string, callback: (...args: unknown[]) => void) => void;
  capture?: { photo?: { source?: string; format?: string; upscale?: number } };
  takeScreenshot?: (source: string, format: string, upscale: number) => Promise<{ blob: Blob; format: string }>;
  gameManager?: {
    FS?: { analyzePath: (path: string) => { exists: boolean }; mkdir: (path: string) => void; writeFile: (path: string, bytes: Uint8Array) => void; unlink: (path: string) => void };
    getFrameNum?: () => number;
    getDiskCount?: () => number;
    getCurrentDisk?: () => number;
    getState?: () => Uint8Array;
    getSaveFile?: () => Promise<Uint8Array>;
    getSaveFilePath?: () => string;
    getVideoDimensions?: (dimension: "aspect" | "width" | "height") => number | undefined;
    loadSaveFiles?: () => void;
    loadState?: (bytes: Uint8Array) => void;
    setCurrentDisk?: (index: number) => void;
    simulateInput?: (player: number, control: number, value: 0 | 1) => void;
    toggleMainLoop?: (running: boolean) => void;
  };
  downloadType?: { rom?: { dontExtractIfCore?: string[] } };
};

export type ManualScreenshot = { screenshot: Blob; format: string };

export async function captureManualScreenshot(instance: EmulatorInstance): Promise<ManualScreenshot> {
  if (!instance.takeScreenshot) throw new Error("PLAYER_SCREENSHOT_UNAVAILABLE");
  const photo = instance.capture?.photo;
  const result = await instance.takeScreenshot(photo?.source ?? "canvas", photo?.format ?? "png", photo?.upscale ?? 1);
  if (!result.blob || typeof result.blob.size !== "number" || result.blob.size === 0) throw new Error("PLAYER_SCREENSHOT_EMPTY");
  return { screenshot: result.blob, format: result.format || "png" };
}

export function captureManualState(instance: EmulatorInstance, capture: ManualScreenshot) {
  const state = instance.gameManager?.getState?.();
  // The runtime lives in a same-origin iframe; realm-local instanceof checks
  // reject its otherwise valid Uint8Array and Blob values.
  if (!state || !ArrayBuffer.isView(state) || state.byteLength === 0) throw new Error("PLAYER_STATE_UNAVAILABLE");
  if (!capture.screenshot || typeof capture.screenshot.size !== "number" || capture.screenshot.size === 0) throw new Error("PLAYER_SCREENSHOT_EMPTY");
  return { ...capture, state: new Uint8Array(state) };
}

export type AdapterCallbacks = {
  onReady?: (emulator: EmulatorInstance) => void;
  onGameStart?: () => void | boolean;
  onSaveState?: (payload: { screenshot: Blob; format: string; state: Uint8Array }) => void;
  onSaveSave?: (payload: { screenshot: Blob; format: string; save: Uint8Array }) => void;
};

type EJSGameManagerConstructor = {
  prototype?: {
    writeFile?: (path: string, data: unknown) => unknown;
  };
};

declare global {
  interface Window {
    EJS_player?: string;
    EJS_core?: string;
    EJS_gameUrl?: string;
    EJS_gameName?: string;
    EJS_gameID?: number;
    EJS_pathtodata?: string;
    EJS_biosUrl?: string;
    EJS_loadStateURL?: string;
    EJS_startOnLoaded?: boolean;
    EJS_dontExtractRom?: boolean;
    EJS_disableBatchBootup?: boolean;
    EJS_language?: string;
    EJS_disableAutoLang?: boolean;
    EJS_threads?: boolean;
    EJS_defaultOptions?: Record<string, string>;
    EJS_paths?: Record<string, string>;
    EJS_externalFiles?: Record<string, string>;
    EJS_gameParentUrl?: string;
    EJS_fullscreenOnLoaded?: boolean;
    EJS_disableDatabases?: boolean;
    EJS_disableLocalStorage?: boolean;
    EJS_CacheLimit?: number;
    EJS_Buttons?: Record<string, boolean | { visible?: boolean }>;
    EJS_onGameStart?: () => void;
    EJS_ready?: () => void;
    EJS_onSaveState?: (payload: { screenshot: Blob; format: string; state: Uint8Array }) => void;
    EJS_onSaveSave?: (payload: { screenshot: Blob; format: string; save: Uint8Array }) => void;
    EJS_emulator?: EmulatorInstance;
    EJS_GameManager?: EJSGameManagerConstructor;
  }
}

export const adapterID = "ejs-4.2.3-v2";

const supportedAdapters: Record<string, string> = {
  "4.2.3": adapterID,
  "4.3.0-pre": "ejs-4.3.0-pre-v1"
};

const normalizedExternalFileWriters = new WeakSet<(...args: never[]) => unknown>();

function normalizeExternalFileWrites(constructor: EJSGameManagerConstructor | undefined) {
  const prototype = constructor?.prototype;
  const original = prototype?.writeFile;
  if (!prototype || typeof original !== "function") throw new Error("PLAYER_EXTERNAL_FILES_COMPATIBILITY_UNAVAILABLE");
  if (normalizedExternalFileWriters.has(original as (...args: never[]) => unknown)) return;
  const normalizedWriteFile = function (this: unknown, path: string, data: unknown) {
    const bytes = Object.prototype.toString.call(data) === "[object ArrayBuffer]"
      ? new Uint8Array(data as ArrayBuffer)
      : data;
    return original.call(this, path, bytes);
  };
  normalizedExternalFileWriters.add(normalizedWriteFile as (...args: never[]) => unknown);
  prototype.writeFile = normalizedWriteFile;
}

function installExternalFileCompatibility(runtimeWindow: typeof window) {
  const previous = Object.getOwnPropertyDescriptor(runtimeWindow, "EJS_GameManager");
  if (previous && !previous.configurable) {
    normalizeExternalFileWrites(runtimeWindow.EJS_GameManager);
    return () => undefined;
  }

  let current = runtimeWindow.EJS_GameManager;
  if (current) normalizeExternalFileWrites(current);
  Object.defineProperty(runtimeWindow, "EJS_GameManager", {
    configurable: true,
    enumerable: previous?.enumerable ?? true,
    get: () => current,
    set: (value: EJSGameManagerConstructor | undefined) => {
      normalizeExternalFileWrites(value);
      current = value;
    }
  });

  return () => {
    if (current) {
      Object.defineProperty(runtimeWindow, "EJS_GameManager", {
        configurable: true,
        enumerable: previous?.enumerable ?? true,
        writable: true,
        value: current
      });
    } else if (previous) {
      Object.defineProperty(runtimeWindow, "EJS_GameManager", previous);
    } else {
      Reflect.deleteProperty(runtimeWindow, "EJS_GameManager");
    }
  };
}

function safeVirtualPath(value: string) {
  if (!value.startsWith("/") || value.length > 512 || value.includes("\\") || value.includes("?") || value.includes("#") || value.includes("//")) return false;
  return value.slice(1).split("/").every((segment) => segment !== "" && segment !== "." && segment !== "..");
}

function validatedExternalFiles(config: PlayerConfig): Record<string, string> {
  const entries = Object.entries(config.externalFiles);
  if (entries.length > 16) throw new Error("PLAYER_EXTERNAL_FILES_INVALID");
  const result: Record<string, string> = {};
  for (const [virtualPath, source] of entries) {
    const externalPrefix = `/runtime/launches/${config.launchId}/external-files/`;
    const logicalName = source.startsWith(externalPrefix) ? source.slice(externalPrefix.length) : "";
    if (!safeVirtualPath(virtualPath) || source.length > 1024 ||
      !source.startsWith(externalPrefix) || !/^[A-Za-z0-9_(). -]{1,255}$/.test(logicalName) ||
      logicalName === "." || logicalName === "..") {
      throw new Error("PLAYER_EXTERNAL_FILES_INVALID");
    }
    result[virtualPath] = source;
  }
  return result;
}

function validateDiscSet(config: PlayerConfig, externalFiles: Record<string, string>) {
  const discSet = config.discSet;
  if (discSet === undefined || discSet === null) return;
  if (discSet.contentKind !== "MULTI_DISC_M3U_V1" || !Number.isInteger(discSet.count) ||
    discSet.count < 2 || discSet.count > 8 || !Array.isArray(discSet.entries) || discSet.entries.length !== discSet.count ||
    !Number.isInteger(discSet.initialDiscIndex) || discSet.initialDiscIndex < 0 ||
    discSet.initialDiscIndex >= discSet.count ||
    config.gameUrl !== `/runtime/launches/${config.launchId}/game/playlist.m3u`) {
    throw new Error("PLAYER_DISC_SET_INVALID");
  }
  for (let index = 0; index < discSet.count; index += 1) {
    const entry = discSet.entries[index];
    const canonicalName = `disc-${String(index + 1).padStart(3, "0")}.chd`;
    if (!entry || entry.index !== index || entry.label !== `光盘 ${index + 1}` ||
      entry.virtualPath !== `/${canonicalName}` ||
      externalFiles[entry.virtualPath] !== `/runtime/launches/${config.launchId}/external-files/${canonicalName}`) {
      throw new Error("PLAYER_DISC_SET_INVALID");
    }
  }
}

export type DiscState = { count: number; currentIndex: number };

export function readDiscState(instance: EmulatorInstance, expectedCount?: number): DiscState {
  const count = instance.gameManager?.getDiskCount?.();
  const currentIndex = instance.gameManager?.getCurrentDisk?.();
  if (!Number.isInteger(count) || !Number.isInteger(currentIndex) || count === undefined || currentIndex === undefined ||
    count < 2 || count > 8 || currentIndex < 0 || currentIndex >= count ||
    expectedCount !== undefined && count !== expectedCount) {
    throw new Error("PLAYER_DISC_SET_INVALID");
  }
  return { count, currentIndex };
}

export function switchDisc(instance: EmulatorInstance, targetIndex: number, expectedCount: number): DiscState {
  if (!Number.isInteger(targetIndex) || targetIndex < 0 || targetIndex >= expectedCount) {
    throw new Error("PLAYER_DISC_INDEX_INVALID");
  }
  const before = readDiscState(instance, expectedCount);
  if (targetIndex === before.currentIndex) return before;
  const setCurrentDisk = instance.gameManager?.setCurrentDisk?.bind(instance.gameManager);
  if (!setCurrentDisk) throw new Error("PLAYER_DISC_SWITCH_UNAVAILABLE");
  setCurrentDisk(targetIndex);
  const after = readDiscState(instance, expectedCount);
  if (after.currentIndex !== targetIndex) throw new Error("PLAYER_DISC_SWITCH_FAILED");
  return after;
}

function initializeMultiDiscSettings(instance: EmulatorInstance) {
  if (instance.allSettings === undefined) {
    instance.allSettings = {};
    return;
  }
  if (Object.prototype.toString.call(instance.allSettings) !== "[object Object]") {
    throw new Error("PLAYER_DISC_SETTINGS_INVALID");
  }
}
function validateConfig(config: PlayerConfig) {
  if (!/^[a-z0-9_]{1,64}$/.test(config.runtimeCore)) throw new Error("PLAYER_RUNTIME_CORE_INVALID");
  if (!config.runtimePathOverrides || Object.entries(config.runtimePathOverrides).length !== 1 || Object.entries(config.runtimePathOverrides).some(([name, source]) =>
    !/^[A-Za-z0-9_.-]+-wasm\.data$/.test(name) || name.includes("..") ||
    !source.startsWith(`/runtime/emulatorjs/${config.emulatorjsVersion}/`))) throw new Error("PLAYER_RUNTIME_PATHS_INVALID");
  if (!["SINGLE_FILE", "DOS_OVERLAY", "NONE"].includes(config.persistentSaveMode) ||
    (config.persistentSaveMode === "NONE") !== (config.persistentSaveUrl === null) ||
    config.persistentSaveMode !== "NONE" && config.persistentSaveUrl !== `/runtime/launches/${config.launchId}/persistent-save`) {
    throw new Error("PLAYER_PERSISTENT_CAPABILITY_INVALID");
  }
  if (config.inputMode !== "STANDARD" && config.inputMode !== "POINTER") throw new Error("PLAYER_INPUT_MODE_INVALID");
  if (!config.defaultCoreOptions || Object.entries(config.defaultCoreOptions).length > 32 ||
    Object.entries(config.defaultCoreOptions).some(([name, value]) => ["__proto__", "constructor", "prototype"].includes(name) ||
      !/^[\x20-\x7E]{1,128}$/.test(name) || !/^[\x20-\x7E]{0,128}$/.test(value))) throw new Error("PLAYER_CORE_OPTIONS_INVALID");
  if (!Array.isArray(config.startupActions) || config.startupActions.length > 4 || config.startupActions.some((action) =>
    action.event !== "GAME_START" || action.kind !== "PRESS_CONTROL" ||
    !Number.isInteger(action.delayMs) || action.delayMs < 0 || action.delayMs > 10_000 ||
    !Number.isInteger(action.player) || action.player < 0 || action.player > 3 ||
    !Number.isInteger(action.control) || action.control < 0 || action.control > 255 ||
    !Number.isInteger(action.durationMs) || action.durationMs < 1 || action.durationMs > 1_000)) {
    throw new Error("PLAYER_STARTUP_ACTION_INVALID");
  }
}

function startWhenAvailable(runtimeWindow: Window) {
  const immediate = runtimeWindow.document.querySelector<HTMLElement>(".ejs_start_button");
  if (immediate) {
    immediate.click();
    return () => undefined;
  }
  const Observer = runtimeWindow.document.defaultView?.MutationObserver;
  if (!Observer) {
    runtimeWindow.dispatchEvent(new ErrorEvent("error", { error: new Error("PLAYER_DOS_START_UNAVAILABLE") }));
    return () => undefined;
  }
  const observer = new Observer(() => {
    const startButton = runtimeWindow.document.querySelector<HTMLElement>(".ejs_start_button");
    if (!startButton) return;
    observer.disconnect();
    runtimeWindow.clearTimeout(timeout);
    startButton.click();
  });
  observer.observe(runtimeWindow.document.documentElement, { childList: true, subtree: true });
  const timeout = runtimeWindow.setTimeout(() => {
    observer.disconnect();
    runtimeWindow.dispatchEvent(new ErrorEvent("error", { error: new Error("PLAYER_DOS_START_UNAVAILABLE") }));
  }, 30_000);
  return () => {
    observer.disconnect();
    runtimeWindow.clearTimeout(timeout);
  };
}

export function scheduleStartupActions(config: PlayerConfig, instance: EmulatorInstance, timerWindow: Window = window) {
  const gameManager = instance.gameManager;
  const simulate = gameManager?.simulateInput?.bind(gameManager);
  if (config.startupActions.length === 0) return () => undefined;
  if (!simulate) throw new Error("PLAYER_STARTUP_ACTION_UNAVAILABLE");
  const timers: number[] = [];
  const pressed = new Map<string, { player: number; control: number }>();
  for (const action of config.startupActions) {
    timers.push(timerWindow.setTimeout(() => {
      const key = `${action.player}:${action.control}`;
      simulate(action.player, action.control, 1);
      pressed.set(key, action);
      timers.push(timerWindow.setTimeout(() => {
        simulate(action.player, action.control, 0);
        pressed.delete(key);
      }, action.durationMs));
    }, action.delayMs));
  }
  return () => {
    for (const timer of timers) timerWindow.clearTimeout(timer);
    for (const action of pressed.values()) simulate(action.player, action.control, 0);
    pressed.clear();
  };
}

export function mountEmulatorJS(config: PlayerConfig, target: HTMLElement, callbacks: AdapterCallbacks = {}, playerWindow: Window = window) {
  if (supportedAdapters[config.emulatorjsVersion] !== config.playerAdapterId) throw new Error("PLAYER_ADAPTER_MISMATCH");
  validateConfig(config);
  const externalFiles = validatedExternalFiles(config);
  validateDiscSet(config, externalFiles);
  target.id = "retrom-emulator";
  const runtimeWindow = playerWindow as typeof window;
  runtimeWindow.EJS_player = "#retrom-emulator";
  runtimeWindow.EJS_core = config.runtimeCore;
  runtimeWindow.EJS_gameUrl = config.gameUrl;
  runtimeWindow.EJS_gameName = config.gameName;
  runtimeWindow.EJS_gameID = config.emulatorGameId;
  runtimeWindow.EJS_pathtodata = config.runtimeBaseUrl;
  runtimeWindow.EJS_biosUrl = config.biosUrl ?? undefined;
  runtimeWindow.EJS_gameParentUrl = config.parentUrl ?? undefined;
  runtimeWindow.EJS_loadStateURL = config.discSet ? undefined : config.stateUrl ?? undefined;
  const deferredDOSStart = config.emulatorjsVersion === "4.3.0-pre" && config.runtimeCore === "dosbox_pure";
  runtimeWindow.EJS_startOnLoaded = !deferredDOSStart;
  runtimeWindow.EJS_dontExtractRom = deferredDOSStart;
  runtimeWindow.EJS_disableBatchBootup = deferredDOSStart;
  runtimeWindow.EJS_language = "zh-CN";
  runtimeWindow.EJS_disableAutoLang = false;
  runtimeWindow.EJS_threads = config.requiresThreads;
  runtimeWindow.EJS_fullscreenOnLoaded = false;
  runtimeWindow.EJS_disableDatabases = true;
  runtimeWindow.EJS_disableLocalStorage = true;
  runtimeWindow.EJS_CacheLimit = 0;
  runtimeWindow.EJS_Buttons = { exitEmulation: false };
  let cleanupDeferredStart: () => void = () => undefined;
  runtimeWindow.EJS_ready = () => {
    const instance = runtimeWindow.EJS_emulator;
    if (!instance) {
      if (config.discSet) throw new Error("PLAYER_DISC_API_UNAVAILABLE");
      return;
    }
    if (config.discSet) initializeMultiDiscSettings(instance);
    if (deferredDOSStart) {
      const dontExtractIfCore = instance.downloadType?.rom?.dontExtractIfCore;
      if (!Array.isArray(dontExtractIfCore)) throw new Error("PLAYER_DOS_ARCHIVE_MODE_UNAVAILABLE");
      if (!dontExtractIfCore.includes(config.runtimeCore)) dontExtractIfCore.push(config.runtimeCore);
    }
    callbacks.onReady?.(instance);
    if (deferredDOSStart) cleanupDeferredStart = startWhenAvailable(runtimeWindow);
  };
  let startupScheduled = false;
  let cleanupStartup: () => void = () => undefined;
  runtimeWindow.EJS_onGameStart = () => {
    if (callbacks.onGameStart?.() === false) return;
    if (!startupScheduled && runtimeWindow.EJS_emulator) {
      startupScheduled = true;
      cleanupStartup = scheduleStartupActions(config, runtimeWindow.EJS_emulator, runtimeWindow);
    }
  };
  runtimeWindow.EJS_onSaveState = callbacks.onSaveState;
  runtimeWindow.EJS_onSaveSave = config.persistentSaveMode === "NONE" ? undefined : callbacks.onSaveSave;
  runtimeWindow.EJS_defaultOptions = { ...config.defaultCoreOptions };
  runtimeWindow.EJS_paths = { ...config.runtimePathOverrides };
  runtimeWindow.EJS_externalFiles = externalFiles;
  const cleanupExternalFileCompatibility = config.emulatorjsVersion === "4.2.3" && Object.keys(externalFiles).length > 0
    ? installExternalFileCompatibility(runtimeWindow)
    : () => undefined;
  const script = runtimeWindow.document.createElement("script");
  script.src = config.loaderUrl;
  script.async = true;
  script.dataset.retromLoader = "true";
  runtimeWindow.document.head.append(script);
  return () => { cleanupDeferredStart(); cleanupStartup(); script.remove(); cleanupExternalFileCompatibility(); };
}
