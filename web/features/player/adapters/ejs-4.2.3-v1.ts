export type PlayerConfig = {
  launchId: string;
  emulatorjsVersion: string;
  playerAdapterId: string;
  core: string;
  emulatorGameId: number;
  gameName: string;
  runtimeBaseUrl: string;
  loaderUrl: string;
  gameUrl: string;
  biosUrl: string | null;
  parentUrl: string | null;
  stateUrl: string | null;
  persistentSaveUrl: string;
  requiresThreads: boolean;
  runtimePathOverrides: Record<string, string>;
  defaultCoreOptions: Record<string, string>;
  externalFiles: Record<string, string>;
  dosEntry?: string | null;
  warnings?: string[];
  returnTo: string;
};

export type EmulatorInstance = {
  paused?: boolean;
  on: (event: string, callback: (...args: unknown[]) => void) => void;
  capture?: { photo?: { source?: string; format?: string; upscale?: number } };
  takeScreenshot?: (source: string, format: string, upscale: number) => Promise<{ blob: Blob; format: string }>;
  gameManager?: {
    FS?: { analyzePath: (path: string) => { exists: boolean }; mkdir: (path: string) => void; writeFile: (path: string, bytes: Uint8Array) => void; unlink: (path: string) => void };
    getFrameNum?: () => number;
    getState?: () => Uint8Array;
    getSaveFile?: () => Promise<Uint8Array>;
    getSaveFilePath?: () => string;
    loadSaveFiles?: () => void;
    toggleMainLoop?: (running: boolean) => void;
  };
};

export async function captureManualState(instance: EmulatorInstance) {
  const state = instance.gameManager?.getState?.();
  // The runtime lives in a same-origin iframe; realm-local instanceof checks
  // reject its otherwise valid Uint8Array and Blob values.
  if (!state || !ArrayBuffer.isView(state) || state.byteLength === 0) throw new Error("PLAYER_STATE_UNAVAILABLE");
  if (!instance.takeScreenshot) throw new Error("PLAYER_SCREENSHOT_UNAVAILABLE");
  const photo = instance.capture?.photo;
  const result = await instance.takeScreenshot(photo?.source ?? "canvas", photo?.format ?? "png", photo?.upscale ?? 1);
  if (!result.blob || typeof result.blob.size !== "number" || result.blob.size === 0) throw new Error("PLAYER_SCREENSHOT_EMPTY");
  return { screenshot: result.blob, format: result.format || "png", state: new Uint8Array(state) };
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
    EJS_onGameStart?: () => void;
    EJS_ready?: () => void;
    EJS_onSaveState?: (payload: { screenshot: Blob; format: string; state: Uint8Array }) => void;
    EJS_onSaveSave?: (payload: { screenshot: Blob; format: string; save: Uint8Array }) => void;
    EJS_emulator?: EmulatorInstance;
  }
}

export const adapterID = "ejs-4.2.3-v1";

function validatedExternalFiles(config: PlayerConfig): Record<string, string> {
  const entries = Object.entries(config.externalFiles);
  const expectedURL = `/runtime/launches/${config.launchId}/dos-config/game.conf`;
  const expectsDOSConfig = config.core === "dosbox_pure" && config.dosEntry != null && config.defaultCoreOptions.dosbox_pure_conf === "outside";
  if (entries.length === 0 && !expectsDOSConfig) return {};
  if (entries.length !== 1 || entries[0][0] !== "/game.conf" || entries[0][1] !== expectedURL || !expectsDOSConfig) {
    throw new Error("PLAYER_EXTERNAL_FILES_INVALID");
  }
  return { "/game.conf": expectedURL };
}

export function mountEmulatorJS(config: PlayerConfig, target: HTMLElement, callbacks: AdapterCallbacks = {}, playerWindow: Window = window) {
  if (config.playerAdapterId !== adapterID || config.emulatorjsVersion !== "4.2.3") throw new Error("PLAYER_ADAPTER_MISMATCH");
  const externalFiles = validatedExternalFiles(config);
  target.id = "retrom-emulator";
  const runtimeWindow = playerWindow as typeof window;
  runtimeWindow.EJS_player = "#retrom-emulator";
  runtimeWindow.EJS_core = config.core;
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
  runtimeWindow.EJS_ready = () => runtimeWindow.EJS_emulator && callbacks.onReady?.(runtimeWindow.EJS_emulator);
  runtimeWindow.EJS_onGameStart = callbacks.onGameStart;
  runtimeWindow.EJS_onSaveState = callbacks.onSaveState;
  runtimeWindow.EJS_onSaveSave = callbacks.onSaveSave;
  runtimeWindow.EJS_defaultOptions = { ...config.defaultCoreOptions };
  runtimeWindow.EJS_paths = { ...config.runtimePathOverrides };
  runtimeWindow.EJS_externalFiles = externalFiles;
  const script = runtimeWindow.document.createElement("script");
  script.src = config.loaderUrl;
  script.async = true;
  script.dataset.retromLoader = "true";
  runtimeWindow.document.head.append(script);
  return () => script.remove();
}
