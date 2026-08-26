import { installEmulatorJs423NetplayCompatibility } from "../netplay/ejs-netplay-4.2.3-v1";
import {
  installEmulatorJs423StateRestoreCompatibility,
  requiresExplicitStateRestore,
} from "../explicit-state-restore";
import {
  installDOSBoxPureStateCompatibility,
  requiresDOSBoxPureStateCompatibility,
} from "../dosbox-pure-state";
import { retromShaders } from "../retrom-shaders";
import { createRetromDefaultControls, type EmulatorDefaultControls } from "../keyboard-controls";
import { installImmersiveGamepadFilter, type ImmersiveGamepadFilter } from "../immersive-gamepad-filter";
import type { NetplayProfile } from "../netplay/controller";
import {
  initializeMultiDiscSettings,
  validateConfig,
  validatedExternalFiles,
  validateDiscSet,
} from "./ejs-config";
import { installArchiveWorkerCompatibility } from "./archive-worker-compatibility";

export {
  captureManualScreenshot,
  captureManualState,
  captureReviewScreenshot,
  coreFramebufferNeedsCanvasOrientation,
  type ManualScreenshot,
} from "./ejs-screenshot";
export {
  readDiscState,
  switchDisc,
  switchDiscPreservingPause,
  validateConfig,
  type DiscState,
} from "./ejs-config";

export type PlayerConfig = {
  mode: "single" | "netplay";
  launchId: string;
  emulatorjsVersion: string;
  playerAdapterId: string;
  core: string;
  runtimeCore: string;
  coreName: string;
  coreArtifactId: string;
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
  netplay: {
    roomId: string;
    sessionId: string;
    playerNo: number;
    netplayProfile: NetplayProfile;
    runtimeSocketUrl: string;
  } | null;
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
  Module?: { postMainLoop?: () => void };
  canvas?: HTMLCanvasElement;
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
  changeSettingOption?: (name: string, value: string) => void;
  enableShader?: (name: string) => void;
  on: (event: string, callback: (...args: unknown[]) => void) => void;
  capture?: { photo?: { source?: string; format?: string; upscale?: number } };
  takeScreenshot?: (source: string, format: string, upscale: number) => Promise<{ blob: Blob; format: string }>;
  gameManager?: {
    savePayloadKind?: "RUNTIME_STATE" | "NATIVE_SAVE_BUNDLE_V1";
    validationPurpose?: boolean;
    getRpgPosition?: () => { mapId: number; playerX: number; playerY: number; fixtureState: number };
    getCheckpointAvailability?: () => { available: boolean; reason: string | null };
    FS?: {
      analyzePath: (path: string) => { exists: boolean };
      mkdir: (path: string) => void;
      writeFile: (path: string, bytes: Uint8Array) => void;
      unlink: (path: string) => void;
      readFile?: (path: string) => ArrayBufferView;
      stat?: (path: string) => { mode: number; size: number; mtime?: Date | number; ctime?: Date | number };
      lstat?: (path: string) => { mode: number; size: number; mtime?: Date | number; ctime?: Date | number };
      readdir?: (path: string) => string[];
      rmdir?: (path: string) => void;
      isDir?: (mode: number) => boolean;
      isFile?: (mode: number) => boolean;
    };
    getFrameNum?: () => number;
    getDiskCount?: () => number;
    getCurrentDisk?: () => number;
    getState?: () => Uint8Array;
    getStateAsync?: () => Promise<Uint8Array>;
    getVideoDimensions?: (dimension: "aspect" | "width" | "height") => number | undefined;
    loadState?: (bytes: Uint8Array) => void;
    setCurrentDisk?: (index: number) => void;
    simulateInput?: (player: number, control: number, value: number) => void;
    toggleMainLoop?: (running: boolean) => void;
    toggleFastForward?: (running: boolean) => void;
    functions?: {
      loadState?: (path: string, slot: number) => unknown;
      restart?: () => void;
      saveStateInfo?: () => string;
      simulateInput?: (player: number, control: number, value: number) => void;
      screenshot?: () => void;
    };
    loadStateAndWait?: (bytes: Uint8Array, timeoutMs?: number) => Promise<{ byteExact: boolean }>;
    loadExplicitStateAndWait?: (bytes: Uint8Array, timeoutMs?: number) => Promise<void>;
    runNetplayFrame?: (timeoutMs?: number) => Promise<number>;
  };
  downloadType?: { rom?: { dontExtractIfCore?: string[] } };
};

export type ManualStatePayload = {
  screenshot: Blob;
  format: string;
  state: Uint8Array;
  payloadKind?: "RUNTIME_STATE" | "NATIVE_SAVE_BUNDLE_V1";
  validationPurpose?: boolean;
};

export type AdapterCallbacks = {
  onReady?: (emulator: EmulatorInstance) => void;
  onGameStart?: () => void | boolean;
  onSaveState?: (payload: ManualStatePayload) => void;
};

export type AdapterMountOptions = {
  immersiveGamepadFilter?: ImmersiveGamepadFilter;
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
    EJS_DEBUG_XX?: boolean;
    EJS_EXPERIMENTAL_NETPLAY?: boolean;
    EJS_threads?: boolean;
    EJS_defaultOptions?: Record<string, string>;
    EJS_shaders?: typeof retromShaders;
    EJS_paths?: Record<string, string>;
    EJS_externalFiles?: Record<string, string>;
    EJS_gameParentUrl?: string;
    EJS_fullscreenOnLoaded?: boolean;
    EJS_disableDatabases?: boolean;
    EJS_disableLocalStorage?: boolean;
    EJS_CacheLimit?: number;
    EJS_Buttons?: Record<string, boolean | { visible?: boolean }>;
    EJS_defaultControls?: EmulatorDefaultControls;
    EJS_onGameStart?: () => void;
    EJS_ready?: () => void;
    EJS_onSaveState?: (payload: ManualStatePayload) => void;
    EJS_onSaveSave?: (payload: { screenshot: Blob; format: string; save: Uint8Array }) => void;
    EJS_emulator?: EmulatorInstance;
    EJS_GameManager?: EJSGameManagerConstructor;
  }
}

export const adapterID = "ejs-4.2.3-v3";
export const legacyNetplayAdapterID = "ejs-4.2.3-v2";

const supportedAdapters: Record<string, string> = {
  "4.2.3": adapterID,
  "4.3.0-pre": "ejs-4.3.0-pre-v2"
};

const normalizedExternalFileWriters = new WeakSet<(...args: never[]) => unknown>();

function normalizeExternalFileWrites(constructor: EJSGameManagerConstructor | undefined) {
  const prototype = constructor?.prototype;
  const original = prototype?.writeFile;
  if (!prototype || typeof original !== "function") {throw new Error("PLAYER_EXTERNAL_FILES_COMPATIBILITY_UNAVAILABLE");}
  if (normalizedExternalFileWriters.has(original as (...args: never[]) => unknown)) {return;}
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
  if (current) {normalizeExternalFileWrites(current);}
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
    if (!startButton) {return;}
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
  if (config.startupActions.length === 0) {return () => undefined;}
  if (!simulate) {throw new Error("PLAYER_STARTUP_ACTION_UNAVAILABLE");}
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
    for (const timer of timers) {timerWindow.clearTimeout(timer);}
    for (const action of pressed.values()) {simulate(action.player, action.control, 0);}
    pressed.clear();
  };
}

export function mountEmulatorJS(
  config: PlayerConfig,
  target: HTMLElement,
  callbacks?: AdapterCallbacks,
  playerWindow?: Window,
  options?: AdapterMountOptions,
) {
  return mountEmulatorJSRuntime(config, target, callbacks ?? {}, playerWindow ?? window, options ?? {});
}

function mountEmulatorJSRuntime(
  config: PlayerConfig,
  target: HTMLElement,
  callbacks: AdapterCallbacks,
  playerWindow: Window,
  options: AdapterMountOptions,
) {
  if (!supportsPlayerAdapter(config, options)) {throw new Error("PLAYER_ADAPTER_MISMATCH");}
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
  const explicitStateRestore = requiresExplicitStateRestore(config);
  const needsDOSBoxStateCompatibility = requiresDOSBoxPureStateCompatibility(config);
  runtimeWindow.EJS_loadStateURL = config.discSet || explicitStateRestore ? undefined : config.stateUrl ?? undefined;
  const deferredDOSStart = config.emulatorjsVersion === "4.3.0-pre" && config.runtimeCore === "dosbox_pure";
  runtimeWindow.EJS_startOnLoaded = !deferredDOSStart;
  runtimeWindow.EJS_dontExtractRom = deferredDOSStart;
  runtimeWindow.EJS_disableBatchBootup = deferredDOSStart;
  runtimeWindow.EJS_language = "zh-CN";
  runtimeWindow.EJS_disableAutoLang = false;
  // 4.2.3 only forwards RetroArch's native state-task completion log through
  // its auditable source loader. Netplay and every explicitly selected save
  // consume that callback; the EmulatorJS experimental transport stays off.
  runtimeWindow.EJS_DEBUG_XX = config.mode === "netplay" || explicitStateRestore;
  runtimeWindow.EJS_EXPERIMENTAL_NETPLAY = false;
  runtimeWindow.EJS_threads = config.requiresThreads;
  runtimeWindow.EJS_fullscreenOnLoaded = false;
  runtimeWindow.EJS_disableDatabases = true;
  runtimeWindow.EJS_disableLocalStorage = true;
  runtimeWindow.EJS_CacheLimit = 0;
  runtimeWindow.EJS_Buttons = { exitEmulation: false };
  runtimeWindow.EJS_defaultControls = createRetromDefaultControls();
  let cleanupDeferredStart: () => void = () => undefined;
  runtimeWindow.EJS_ready = () => {
    const instance = runtimeWindow.EJS_emulator;
    if (!instance) {
      if (config.discSet) {throw new Error("PLAYER_DISC_API_UNAVAILABLE");}
      return;
    }
    if (config.discSet) {initializeMultiDiscSettings(instance);}
    if (deferredDOSStart) {
      const dontExtractIfCore = instance.downloadType?.rom?.dontExtractIfCore;
      if (!Array.isArray(dontExtractIfCore)) {throw new Error("PLAYER_DOS_ARCHIVE_MODE_UNAVAILABLE");}
      if (!dontExtractIfCore.includes(config.runtimeCore)) {dontExtractIfCore.push(config.runtimeCore);}
    }
    callbacks.onReady?.(instance);
    if (deferredDOSStart) {cleanupDeferredStart = startWhenAvailable(runtimeWindow);}
  };
  let startupScheduled = false;
  let cleanupStartup: () => void = () => undefined;
  runtimeWindow.EJS_onGameStart = () => {
    if (needsDOSBoxStateCompatibility && runtimeWindow.EJS_emulator) {
      compatibility.dosbox?.prepare(runtimeWindow.EJS_emulator);
    }
    if (callbacks.onGameStart?.() === false) {return;}
    if (!startupScheduled && runtimeWindow.EJS_emulator) {
      startupScheduled = true;
      cleanupStartup = scheduleStartupActions(config, runtimeWindow.EJS_emulator, runtimeWindow);
    }
  };
  runtimeWindow.EJS_onSaveState = callbacks.onSaveState;
  runtimeWindow.EJS_onSaveSave = undefined;
  runtimeWindow.EJS_defaultOptions = runtimeDefaultOptions(config);
  runtimeWindow.EJS_shaders = retromShaders;
  runtimeWindow.EJS_paths = { ...config.runtimePathOverrides };
  runtimeWindow.EJS_externalFiles = externalFiles;
  const compatibility = installCompatibilityLayers(
    config, runtimeWindow, externalFiles, explicitStateRestore, needsDOSBoxStateCompatibility,
    options.immersiveGamepadFilter,
  );
  const script = runtimeWindow.document.createElement("script");
  script.src = config.loaderUrl;
  script.async = true;
  script.dataset.retromLoader = "true";
  runtimeWindow.document.head.append(script);
  return () => {
    cleanupDeferredStart(); cleanupStartup(); script.remove(); compatibility.externalFiles();
    compatibility.archiveWorker(); compatibility.stateRestore(); compatibility.dosbox?.cleanup();
    compatibility.netplay(); compatibility.gamepad();
  };
}

function supportsPlayerAdapter(config: PlayerConfig, options: AdapterMountOptions) {
  if (config.mode === "netplay") {
    return config.emulatorjsVersion === "4.2.3" && config.playerAdapterId === legacyNetplayAdapterID &&
      options.immersiveGamepadFilter === undefined;
  }
  return supportedAdapters[config.emulatorjsVersion] === config.playerAdapterId;
}

function runtimeDefaultOptions(config: PlayerConfig) {
  if (config.mode === "netplay" && config.runtimeCore === "fbneo") {
    return { ...config.defaultCoreOptions, "fbneo-hiscores": "disabled" };
  }
  return { ...config.defaultCoreOptions };
}

function installCompatibilityLayers(
  config: PlayerConfig,
  runtimeWindow: typeof window,
  externalFiles: Record<string, string>,
  explicitStateRestore: boolean,
  needsDOSBoxStateCompatibility: boolean,
  immersiveGamepadFilter: ImmersiveGamepadFilter | undefined,
) {
  const compatibility = {
    netplay: config.mode === "netplay" ? installEmulatorJs423NetplayCompatibility(runtimeWindow) : () => undefined,
    dosbox: needsDOSBoxStateCompatibility ? installDOSBoxPureStateCompatibility(runtimeWindow) : null,
    stateRestore: explicitStateRestore && !needsDOSBoxStateCompatibility
      ? installEmulatorJs423StateRestoreCompatibility(runtimeWindow, { waitForSerializable: true })
      : () => undefined,
    archiveWorker: installArchiveWorkerCompatibility(
      runtimeWindow, config.emulatorjsVersion, config.runtimeBaseUrl,
    ),
    externalFiles: config.emulatorjsVersion === "4.2.3" && Object.keys(externalFiles).length > 0
      ? installExternalFileCompatibility(runtimeWindow)
      : () => undefined,
  };
  try {
    return {
      ...compatibility,
      gamepad: immersiveGamepadFilter
        ? installImmersiveGamepadFilter(runtimeWindow, immersiveGamepadFilter)
        : () => undefined,
    };
  } catch (error) {
    compatibility.externalFiles(); compatibility.archiveWorker(); compatibility.stateRestore();
    compatibility.dosbox?.cleanup(); compatibility.netplay();
    throw error;
  }
}
