"use client";

import { useEffect, type Dispatch, type RefObject, type SetStateAction } from "react";
import { mountEmulatorJS, validateConfig, type DiscSet, type DiscState, type EmulatorInstance, type PlayerConfig } from "./adapters/ejs-4.2.3-v2";
import { installCanvasContain } from "./canvas-fit";
import { closeEmulatorSettingsPanels } from "./emulator-settings";
import { prepareMultiDiscLaunch } from "./multi-disc-restore";
import { multiDiscPlayerResultCode, type MultiDiscPlayerEvent } from "./multi-disc-telemetry";
import { clearTransientSaveStorage, isTransientSaveFileSystem } from "./transient-save-storage";
import { requiresExplicitStateRestore } from "./explicit-state-restore";
import { canCreateRecoverableManualState } from "./dosbox-pure-state";
import { installPlayerFrameStyle } from "./player-frame-style";
import { applyVideoRenderingMode, type VideoRenderingMode } from "./video-rendering";
import { mobilePlayerQuery, portraitPlayerQuery, reducePlayerOrientation, waitForStableLandscape, type PlayerOrientationState, type PlayerRuntimeKind } from "./orientation";
import { NetplayController } from "./netplay/controller";
import { digestHex, EJSNetplayFrameBridge } from "./netplay/ejs-netplay-4.2.3-v1";
import { readBoundedResponse, reportsNativeExit } from "./player-shell-model";
import { shouldRevealPlayerControlsForKey } from "./player-controls-visibility";
import type { PlayerDebugRuntime } from "./player-chrome";
import { handlePlayerPauseShortcut } from "./keyboard-controls";

type ShellState = "loading" | "running" | "error";
type SyncTone = "synced" | "busy" | "warning";
type Mutable<T> = { current: T };

export type PlayerBootstrapParams = {
  launchId: string;
  stage: RefObject<HTMLDivElement | null>; frameRef: RefObject<HTMLIFrameElement | null>; emulator: Mutable<EmulatorInstance | undefined>;
  returnTo: Mutable<string>; playerMode: Mutable<PlayerConfig["mode"]>; manualSaveAvailableRef: Mutable<boolean>;
  netplayConfig: Mutable<NonNullable<PlayerConfig["netplay"]> | null>; discSetRef: Mutable<DiscSet | null>;
  orientationStateRef: Mutable<PlayerOrientationState>; videoRenderingModeRef: Mutable<VideoRenderingMode>;
  lastAudibleVolume: Mutable<number>; pausedRef: Mutable<boolean>; started: Mutable<boolean>; finishing: Mutable<boolean>;
  heartbeat: Mutable<number | null>; toastTimer: Mutable<number | null>; netplayController: Mutable<NetplayController | null>; netplayPausedRef: Mutable<boolean>;
  setMessage: Dispatch<SetStateAction<string>>; setState: Dispatch<SetStateAction<ShellState>>; setManualSaveAvailable: Dispatch<SetStateAction<boolean>>;
  setNetplayPlayerNo: Dispatch<SetStateAction<number | null>>; setWarnings: Dispatch<SetStateAction<string[]>>; setGameTitle: Dispatch<SetStateAction<string>>;
  setCoreName: Dispatch<SetStateAction<string>>; setPlatformName: Dispatch<SetStateAction<string>>; setDebugRuntime: Dispatch<SetStateAction<PlayerDebugRuntime>>;
  setDiscSet: Dispatch<SetStateAction<DiscSet | null>>; setDiscState: Dispatch<SetStateAction<DiscState | null>>; setOrientationState: Dispatch<SetStateAction<PlayerOrientationState>>;
  setFrameEnabled: Dispatch<SetStateAction<boolean>>; setSyncText: Dispatch<SetStateAction<string>>; setSyncTone: Dispatch<SetStateAction<SyncTone>>;
  setEmulatorVolume: Dispatch<SetStateAction<number>>; setEmulatorMuted: Dispatch<SetStateAction<boolean>>; setPaused: Dispatch<SetStateAction<boolean>>;
  setNetplayPaused: Dispatch<SetStateAction<boolean>>;
  reportPlayerEvent: (event: MultiDiscPlayerEvent) => void; revealControlsAtTopEdge: (clientY: number) => void; showControls: () => void;
  onKeyboardPause: () => void;
  sendEvent: (kind: "start" | "heartbeat" | "finish") => Promise<void>; uploadManualState: (payload: { screenshot: Blob; format: string; state: Uint8Array }) => Promise<boolean>;
};

type BootstrapResources = {
  cleanup?: () => void; canvasContain?: ReturnType<typeof installCanvasContain>; cleanupFrameControls?: () => void;
  nativeMenuObserver?: MutationObserver; ownedNetplayController?: NetplayController;
};

type MountedContext = {
  params: PlayerBootstrapParams; resources: BootstrapResources; controller: AbortController; config: PlayerConfig;
  frame: HTMLIFrameElement; frameWindow: Window; frameDocument: Document; stateBytes: Uint8Array | null;
};

export function usePlayerBootstrap(params: PlayerBootstrapParams) {
  useEffect(() => {
    const controller = new AbortController();
    const resources: BootstrapResources = {};
    void bootstrapPlayer(params, resources, controller).catch((error: unknown) => handleBootstrapError(error, controller, params));
    return () => cleanupBootstrap(params, resources, controller);
  }, [params]);
}

async function bootstrapPlayer(params: PlayerBootstrapParams, resources: BootstrapResources, controller: AbortController) {
  params.setMessage("正在加载 Core、ROM 与依赖配置…");
  const response = await fetch(`/runtime/launches/${params.launchId}/config`, { credentials: "same-origin", cache: "no-store", signal: controller.signal });
  if (!response.ok) {throw new Error(`LAUNCH_CONFIG_${response.status}`);}
  const config = await response.json() as PlayerConfig;
  validateConfig(config);
  applyConfig(params, config);
  await prepareOrientation(params, config, controller);
  const frame = await prepareFrame(params, controller);
  await describeDiscSet(params, config, controller);
  const stateBytes = await fetchLaunchState(config, controller);
  const mounted = mountFrame(params, resources, controller, config, frame, stateBytes);
  resources.cleanup = mountEmulatorJS(config, mounted.target, createMountCallbacks(mounted.context), mounted.context.frameWindow);
}

function applyConfig(params: PlayerBootstrapParams, config: PlayerConfig) {
  params.returnTo.current = config.returnTo;
  params.playerMode.current = config.mode;
  params.manualSaveAvailableRef.current = canCreateRecoverableManualState(config);
  params.setManualSaveAvailable(params.manualSaveAvailableRef.current);
  params.netplayConfig.current = config.netplay;
  params.setNetplayPlayerNo(config.netplay?.playerNo ?? null);
  params.setWarnings(config.warnings ?? []);
  params.setGameTitle(config.gameTitle);
  params.setCoreName(config.coreName || config.core);
  params.setPlatformName(config.platformName);
  params.setDebugRuntime({ coreId: config.core, coreArtifactId: config.coreArtifactId, emulatorJSVersion: config.emulatorjsVersion, playerAdapterId: config.playerAdapterId, inputMode: config.inputMode, crossOriginIsolated: window.crossOriginIsolated, sharedArrayBuffer: typeof SharedArrayBuffer !== "undefined" });
  params.discSetRef.current = config.discSet ?? null;
  params.setDiscSet(config.discSet ?? null);
  params.setDiscState(null);
}

async function prepareOrientation(params: PlayerBootstrapParams, config: PlayerConfig, controller: AbortController) {
  const mobileQuery = window.matchMedia(mobilePlayerQuery);
  const portraitQuery = window.matchMedia(portraitPlayerQuery);
  const runtimeKind: PlayerRuntimeKind = config.mode === "single" ? "single" : config.netplay?.playerNo === 1 ? "netplay-p1" : "netplay-p2";
  let orientation = reducePlayerOrientation(params.orientationStateRef.current, { type: "config-ready", mobile: mobileQuery.matches, portrait: portraitQuery.matches, runtimeKind });
  params.orientationStateRef.current = orientation.state;
  params.setOrientationState(orientation.state);
  if (orientation.state.phase !== "orientation-blocked") {return;}
  params.setMessage("请横向握持设备开始游戏");
  await waitForStableLandscape(portraitQuery, controller.signal, (portrait) => {
    orientation = reducePlayerOrientation(params.orientationStateRef.current, { type: "orientation-stable", portrait, paused: false });
    params.orientationStateRef.current = orientation.state;
    params.setOrientationState(orientation.state);
  });
}

async function prepareFrame(params: PlayerBootstrapParams, controller: AbortController) {
  params.setFrameEnabled(true);
  for (let attempt = 0; attempt < 12 && !params.frameRef.current; attempt += 1) {
    await new Promise<void>((resolve) => window.requestAnimationFrame(() => resolve()));
    if (controller.signal.aborted) {throw new DOMException("Aborted", "AbortError");}
  }
  if (!params.frameRef.current) {throw new Error("PLAYER_FRAME_UNAVAILABLE");}
  return params.frameRef.current;
}

async function describeDiscSet(params: PlayerBootstrapParams, config: PlayerConfig, controller: AbortController) {
  if (!config.discSet) {return;}
  const sizes = await Promise.all(config.discSet.entries.map(async (entry) => {
    const source = config.externalFiles[entry.virtualPath];
    const head = await fetch(source, { method: "HEAD", credentials: "same-origin", cache: "no-store", signal: controller.signal });
    if (!head.ok) {throw new Error("PLAYER_DISC_SET_INVALID");}
    const size = Number(head.headers.get("content-length") ?? "NaN");
    if (!Number.isSafeInteger(size) || size < 8) {throw new Error("PLAYER_DISC_SET_INVALID");}
    return size;
  }));
  params.setMessage(`正在准备多盘内容 · ${config.discSet.count} 张光盘 · ${formatPlayerBytes(sizes.reduce((total, size) => total + size, 0))}`);
}

async function fetchLaunchState(config: PlayerConfig, controller: AbortController) {
  if (!(config.discSet || requiresExplicitStateRestore(config)) || !config.stateUrl) {return null;}
  const response = await fetch(config.stateUrl, { credentials: "same-origin", cache: "no-store", signal: controller.signal });
  if (!response.ok) {throw new Error("PLAYER_SAVE_STATE_UNAVAILABLE");}
  const stateBytes = await readBoundedResponse(response, 64 * 1024 * 1024);
  if (stateBytes.byteLength === 0) {throw new Error("PLAYER_SAVE_STATE_UNAVAILABLE");}
  return stateBytes;
}

function mountFrame(params: PlayerBootstrapParams, resources: BootstrapResources, controller: AbortController, config: PlayerConfig, frame: HTMLIFrameElement, stateBytes: Uint8Array | null) {
  if (!params.stage.current) {throw new Error("PLAYER_FRAME_UNAVAILABLE");}
  const frameWindow = frame.contentWindow;
  const frameDocument = frame.contentDocument;
  if (!frameWindow || !frameDocument) {throw new Error("PLAYER_FRAME_UNAVAILABLE");}
  frameDocument.documentElement.lang = "zh-CN";
  frameDocument.documentElement.classList.add("retrom-native-menu-locked");
  installPlayerFrameStyle(frameDocument);
  const target = frameDocument.createElement("div");
  target.id = "game";
  frameDocument.body.append(target);
  resources.canvasContain = installCanvasContain(frameDocument, () => params.emulator.current?.gameManager?.getVideoDimensions?.("aspect"));
  resources.cleanupFrameControls = installFrameControls(frameDocument, frame, config.inputMode, params);
  return { target, context: { params, resources, controller, config, frame, frameWindow, frameDocument, stateBytes } satisfies MountedContext };
}

function installFrameControls(document: Document, frame: HTMLIFrameElement, inputMode: string, params: PlayerBootstrapParams) {
  const pointer = (event: PointerEvent) => params.revealControlsAtTopEdge(event.clientY);
  const key = (event: KeyboardEvent) => {
    if (shouldRevealPlayerControlsForKey(event.key)) {params.showControls();}
    handlePlayerPauseShortcut(event, params.onKeyboardPause);
  };
  const click = (event: MouseEvent) => {
    const target = event.target;
    if (target && "closest" in target && typeof target.closest === "function" && target.closest(".ejs_menu_bar,.ejs_popup_container,.ejs_cheat_parent,.ejs_control_bar,button,a,input,select,textarea,[role=button]")) {return;}
    frame.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true }));
    frame.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  };
  document.addEventListener("pointermove", pointer, { passive: true });
  document.addEventListener("keydown", key);
  if (inputMode === "STANDARD") {document.addEventListener("click", click);}
  return () => {
    document.removeEventListener("pointermove", pointer);
    document.removeEventListener("keydown", key);
    if (inputMode === "STANDARD") {document.removeEventListener("click", click);}
  };
}

function createMountCallbacks(context: MountedContext) {
  return {
    onReady: (instance: EmulatorInstance) => handleReady(context, instance),
    onGameStart: () => handleGameStart(context),
    onSaveState: (payload: { screenshot: Blob; format: string; state: Uint8Array }) => {void context.params.uploadManualState(payload);},
  };
}

function handleReady(context: MountedContext, instance: EmulatorInstance) {
  if (context.controller.signal.aborted) {return;}
  const { params, frameDocument, resources } = context;
  params.emulator.current = instance;
  applyVideoRenderingMode(instance, instance.canvas ?? frameDocument.querySelector<HTMLCanvasElement>("canvas"), params.videoRenderingModeRef.current);
  const initialVolume = Math.min(1, Math.max(0, typeof instance.volume === "number" ? instance.volume : 0.5));
  params.setEmulatorVolume(initialVolume);
  params.setEmulatorMuted(instance.muted === true || initialVolume === 0);
  if (initialVolume > 0) {params.lastAudibleVolume.current = initialVolume;}
  lockNativeMenu(frameDocument, resources);
  instance.on("saveState", () => undefined);
  instance.on("saveDatabaseLoaded", () => clearTransientStorage(params, instance));
  instance.on("exit", () => reportNativeExit(params));
}

function lockNativeMenu(document: Document, resources: BootstrapResources) {
  const nativeMenu = document.querySelector<HTMLElement>(".ejs_menu_bar");
  if (!nativeMenu) {return;}
  resources.nativeMenuObserver = new MutationObserver(() => {
    if (nativeMenu.classList.contains("ejs_menu_bar_hidden")) {document.documentElement.classList.add("retrom-native-menu-locked");}
  });
  resources.nativeMenuObserver.observe(nativeMenu, { attributes: true, attributeFilter: ["class"] });
}

function clearTransientStorage(params: PlayerBootstrapParams, instance: EmulatorInstance) {
  const fs = instance.gameManager?.FS;
  if (!fs || !isTransientSaveFileSystem(fs)) {
    params.setState("error"); params.setMessage("LAUNCH_TRANSIENT_SAVE_FS_UNAVAILABLE");
    throw new Error("LAUNCH_TRANSIENT_SAVE_FS_UNAVAILABLE");
  }
  try {clearTransientSaveStorage(fs);}
  catch (error) {params.setState("error"); params.setMessage(error instanceof Error ? error.message : "LAUNCH_TRANSIENT_SAVE_CLEAR_FAILED"); throw error;}
}

function reportNativeExit(params: PlayerBootstrapParams) {
  if (!reportsNativeExit(params.playerMode.current, params.finishing.current)) {return;}
  void params.sendEvent("finish").catch(() => {params.setState("error"); params.setMessage("PLAY_SESSION_EVENT_FAILED");});
}

function handleGameStart(context: MountedContext) {
  const { params, controller, frameDocument, config } = context;
  if (controller.signal.aborted) {return false;}
  frameDocument.documentElement.classList.add("retrom-native-menu-locked");
  params.emulator.current?.menu?.close?.();
  applyVideoRenderingMode(params.emulator.current, params.emulator.current?.canvas ?? frameDocument.querySelector<HTMLCanvasElement>("canvas"), params.videoRenderingModeRef.current);
  if (!prepareContentStart(context)) {return false;}
  if (config.mode === "netplay") {return startNetplay(context);}
  completeSinglePlayerStart(context, false);
  return true;
}

function prepareContentStart(context: MountedContext) {
  try {
    if (context.config.discSet) {return prepareMultiDiscStart(context);}
    if (context.stateBytes && requiresExplicitStateRestore(context.config)) {return restoreSingleDiscState(context);}
    return true;
  } catch (caught) {
    reportContentStartFailure(context, caught);
    return false;
  }
}

function prepareMultiDiscStart(context: MountedContext) {
  const { params, config, stateBytes } = context;
  const discSet = config.discSet;
  if (!discSet) {return true;}
  params.setMessage(`正在切换到光盘 ${discSet.initialDiscIndex + 1}`);
  if (!params.emulator.current) {throw new Error("PLAYER_DISC_API_UNAVAILABLE");}
  const selected = prepareMultiDiscLaunch(params.emulator.current, discSet);
  params.setDiscState(selected);
  params.reportPlayerEvent({ eventType: "START", resultCode: "OK", discCount: discSet.count, observedDiscCount: selected.count });
  if (!stateBytes) {params.emulator.current.gameManager?.toggleMainLoop?.(true); return true;}
  const manager = params.emulator.current.gameManager;
  if (!manager?.loadExplicitStateAndWait) {throw new Error("PLAYER_STATE_RESTORE_COMPATIBILITY_UNAVAILABLE");}
  params.setMessage("正在恢复指定存档");
  void manager.loadExplicitStateAndWait(stateBytes).then(() => {
    params.reportPlayerEvent({ eventType: "SAVE_RESTORE_SUCCESS", resultCode: "OK", discCount: discSet.count, observedDiscCount: selected.count });
    completeSinglePlayerStart(context, true);
  }).catch((caught: unknown) => reportAsyncMultiDiscRestoreFailure(context, caught, discSet));
  return false;
}

function restoreSingleDiscState(context: MountedContext) {
  const manager = context.params.emulator.current?.gameManager;
  if (!manager?.loadExplicitStateAndWait || !context.stateBytes) {throw new Error("PLAYER_STATE_RESTORE_COMPATIBILITY_UNAVAILABLE");}
  context.params.setMessage("正在恢复指定存档");
  void manager.loadExplicitStateAndWait(context.stateBytes).then(() => completeSinglePlayerStart(context, true)).catch(() => {
    if (context.controller.signal.aborted) {return;}
    context.params.setState("error"); context.params.setMessage("PLAYER_SAVE_STATE_RESTORE_FAILED");
  });
  return false;
}

function reportAsyncMultiDiscRestoreFailure(context: MountedContext, caught: unknown, discSet: DiscSet) {
  if (context.controller.signal.aborted) {return;}
  const observedDiscCount = observedRuntimeDiscCount(context.params.emulator.current);
  const resultCode = multiDiscPlayerResultCode(caught, "PLAYER_SAVE_STATE_RESTORE_FAILED");
  context.params.reportPlayerEvent({ eventType: "SAVE_RESTORE_FAILURE", resultCode, discCount: discSet.count, observedDiscCount });
  context.params.setState("error"); context.params.setMessage(resultCode);
}

function reportContentStartFailure(context: MountedContext, caught: unknown) {
  const discSet = context.config.discSet;
  if (discSet) {
    const observedDiscCount = observedRuntimeDiscCount(context.params.emulator.current);
    const resultCode = multiDiscPlayerResultCode(caught, context.stateBytes ? "PLAYER_SAVE_STATE_RESTORE_FAILED" : "PLAYER_DISC_API_UNAVAILABLE");
    if (resultCode === "PLAYER_DISC_SET_INVALID" && observedDiscCount !== null && observedDiscCount !== discSet.count) {
      context.params.reportPlayerEvent({ eventType: "DISK_COUNT_MISMATCH", resultCode, discCount: discSet.count, observedDiscCount });
    }
    if (context.stateBytes) {context.params.reportPlayerEvent({ eventType: "SAVE_RESTORE_FAILURE", resultCode, discCount: discSet.count, observedDiscCount });}
  }
  context.params.setState("error");
  context.params.setMessage(caught instanceof Error ? caught.message : "PLAYER_DISC_SET_INVALID");
}

function completeSinglePlayerStart(context: MountedContext, resumeMainLoop: boolean) {
  const { params, controller, resources, frameWindow } = context;
  if (controller.signal.aborted) {return;}
  if (params.emulator.current) {
    params.emulator.current.paused = false;
    if (resumeMainLoop) {params.emulator.current.gameManager?.toggleMainLoop?.(true);}
  }
  params.pausedRef.current = false;
  params.setPaused(false);
  const orientation = reducePlayerOrientation(params.orientationStateRef.current, { type: "runtime-started", paused: false });
  params.orientationStateRef.current = orientation.state;
  params.setOrientationState(orientation.state);
  frameWindow.requestAnimationFrame(() => resources.canvasContain?.refresh());
  void params.sendEvent("start").then(() => {
    params.setState("running");
    params.setSyncText(params.manualSaveAvailableRef.current ? "可创建存档" : "程序菜单模式不可存档");
    params.setSyncTone(params.manualSaveAvailableRef.current ? "synced" : "warning");
    params.heartbeat.current = window.setInterval(() => {void params.sendEvent("heartbeat");}, 30_000);
  }).catch(() => {params.setState("error"); params.setMessage("PLAY_SESSION_EVENT_FAILED");});
}

function startNetplay(context: MountedContext) {
  const { params, config, controller, resources } = context;
  if (!params.emulator.current || !config.netplay) {throw new Error("PLAYER_NETPLAY_CONFIG_INVALID");}
  try {
    params.netplayController.current?.dispose();
    const holder: { current?: NetplayController } = {};
    const current = () => !controller.signal.aborted && params.netplayController.current === holder.current;
    const created = new NetplayController(config.netplay, "", new EJSNetplayFrameBridge(params.emulator.current), netplayCallbacks(context, current));
    holder.current = created;
    resources.ownedNetplayController = created;
    params.netplayController.current = created;
    params.setMessage("正在建立联机同步屏障…");
    void digestHex(new TextEncoder().encode(JSON.stringify(config.netplay.netplayProfile))).then((digest) => created.setProfileDigest(digest)).then(() => created.start()).catch((caught: unknown) => failNetplayStart(context, created, current, caught));
    return true;
  } catch (caught) {
    params.setState("error"); params.setMessage(caught instanceof Error ? caught.message : "NETPLAY_START_FAILED");
    return false;
  }
}

function netplayCallbacks(context: MountedContext, current: () => boolean) {
  const { params } = context;
  return {
    onStatus: (text: string, tone: SyncTone) => {if (current()) {params.setSyncText(text); params.setSyncTone(tone);}},
    onRunning: () => {
      if (!current()) {return;}
      params.setNetplayPaused(false); params.netplayPausedRef.current = false;
      const orientation = reducePlayerOrientation(params.orientationStateRef.current, { type: "runtime-started", paused: false });
      params.orientationStateRef.current = orientation.state; params.setOrientationState(orientation.state);
      if (params.started.current) {return;}
      void params.sendEvent("start").then(() => {params.setState("running"); params.heartbeat.current = window.setInterval(() => {void params.sendEvent("heartbeat");}, 30_000);}).catch(() => {params.setState("error"); params.setMessage("PLAY_SESSION_EVENT_FAILED");});
    },
    onPaused: () => {if (current()) {params.netplayPausedRef.current = true; params.setNetplayPaused(true);}},
    onEnded: (reason: string) => {
      if (!current()) {return;}
      params.setSyncText("联机已结束"); params.setSyncTone("warning"); params.setMessage(reason);
      void params.sendEvent("finish").catch(() => undefined).finally(() => window.setTimeout(() => window.location.replace(params.returnTo.current), 600));
    },
  };
}

function failNetplayStart(context: MountedContext, created: NetplayController, current: () => boolean, caught: unknown) {
  if (!current()) {created.dispose(); return;}
  created.end();
  context.params.setState("error");
  context.params.setMessage(caught instanceof Error ? caught.message : "NETPLAY_START_FAILED");
}

function handleBootstrapError(error: unknown, controller: AbortController, params: PlayerBootstrapParams) {
  if (controller.signal.aborted) {return;}
  const code = error instanceof Error ? error.message : "启动失败";
  params.setMessage(code === "LAUNCH_CONFIG_401" ? "启动会话不可用，请从游戏详情或存档重新开始。" : code);
  params.setState("error");
}

function cleanupBootstrap(params: PlayerBootstrapParams, resources: BootstrapResources, controller: AbortController) {
  controller.abort(); resources.cleanup?.(); resources.canvasContain?.cleanup(); resources.cleanupFrameControls?.();
  resources.ownedNetplayController?.dispose();
  if (params.netplayController.current === resources.ownedNetplayController) {params.netplayController.current = null;}
  closeEmulatorSettingsPanels(params.emulator.current);
  resources.nativeMenuObserver?.disconnect();
  if (params.heartbeat.current !== null) {window.clearInterval(params.heartbeat.current);}
  if (params.toastTimer.current !== null) {window.clearTimeout(params.toastTimer.current);}
}

function formatPlayerBytes(bytes: number) {
  if (bytes < 1024 * 1024) {return `${(bytes / 1024).toFixed(1)} KiB`;}
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}

function observedRuntimeDiscCount(instance: EmulatorInstance | undefined) {
  const value = instance?.gameManager?.getDiskCount?.();
  return typeof value === "number" && Number.isInteger(value) && value >= -1 && value <= 64 ? value : null;
}
