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
  dosEntry?: string | null;
  warnings?: string[];
  returnTo: string;
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
    getState?: () => Uint8Array;
    getSaveFile?: () => Promise<Uint8Array>;
    getSaveFilePath?: () => string;
    getVideoDimensions?: (dimension: "aspect" | "width" | "height") => number | undefined;
    loadSaveFiles?: () => void;
    simulateInput?: (player: number, control: number, value: 0 | 1) => void;
    toggleMainLoop?: (running: boolean) => void;
  };
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
  onGameStart?: () => void;
  onSaveState?: (payload: { screenshot: Blob; format: string; state: Uint8Array }) => void;
  onSaveSave?: (payload: { screenshot: Blob; format: string; save: Uint8Array }) => void;
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
  }
}

export const adapterID = "ejs-4.2.3-v1";

function safeVirtualPath(value: string) {
  if (!value.startsWith("/") || value.length > 512 || value.includes("\\") || value.includes("?") || value.includes("#") || value.includes("//")) return false;
  return value.slice(1).split("/").every((segment) => segment !== "" && segment !== "." && segment !== "..");
}

function validatedExternalFiles(config: PlayerConfig): Record<string, string> {
  const entries = Object.entries(config.externalFiles);
  if (entries.length > 16) throw new Error("PLAYER_EXTERNAL_FILES_INVALID");
  const result: Record<string, string> = {};
  for (const [virtualPath, source] of entries) {
    const dosURL = `/runtime/launches/${config.launchId}/dos-config/game.conf`;
    const externalPrefix = `/runtime/launches/${config.launchId}/external-files/`;
    const logicalName = source.startsWith(externalPrefix) ? source.slice(externalPrefix.length) : "";
    if (!safeVirtualPath(virtualPath) || source.length > 1024 ||
      source !== dosURL && (!source.startsWith(externalPrefix) || !/^[A-Za-z0-9_(). -]{1,255}$/.test(logicalName) ||
        logicalName === "." || logicalName === "..")) {
      throw new Error("PLAYER_EXTERNAL_FILES_INVALID");
    }
    result[virtualPath] = source;
  }
  return result;
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
  if (config.playerAdapterId !== adapterID || config.emulatorjsVersion !== "4.2.3") throw new Error("PLAYER_ADAPTER_MISMATCH");
  validateConfig(config);
  const externalFiles = validatedExternalFiles(config);
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
  runtimeWindow.EJS_loadStateURL = config.stateUrl ?? undefined;
  runtimeWindow.EJS_startOnLoaded = true;
  runtimeWindow.EJS_language = "zh-CN";
  runtimeWindow.EJS_disableAutoLang = false;
  runtimeWindow.EJS_threads = config.requiresThreads;
  runtimeWindow.EJS_fullscreenOnLoaded = false;
  runtimeWindow.EJS_disableDatabases = true;
  runtimeWindow.EJS_disableLocalStorage = true;
  runtimeWindow.EJS_CacheLimit = 0;
  runtimeWindow.EJS_Buttons = { exitEmulation: false };
  runtimeWindow.EJS_ready = () => runtimeWindow.EJS_emulator && callbacks.onReady?.(runtimeWindow.EJS_emulator);
  let startupScheduled = false;
  let cleanupStartup: () => void = () => undefined;
  runtimeWindow.EJS_onGameStart = () => {
    callbacks.onGameStart?.();
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
  const script = runtimeWindow.document.createElement("script");
  script.src = config.loaderUrl;
  script.async = true;
  script.dataset.retromLoader = "true";
  runtimeWindow.document.head.append(script);
  return () => { cleanupStartup(); script.remove(); };
}
