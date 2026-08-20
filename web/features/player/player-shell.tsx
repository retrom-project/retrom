"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useAuth } from "@/features/auth/auth-provider";
import { newUuid, sha256 } from "@/lib/crypto";
import { writeHeaders } from "@/lib/api/client";
import { captureManualScreenshot, captureManualState, mountEmulatorJS, readDiscState, switchDiscPreservingPause, validateConfig, type DiscSet, type DiscState, type EmulatorInstance, type ManualScreenshot, type PlayerConfig } from "./adapters/ejs-4.2.3-v2";
import { installCanvasContain } from "./canvas-fit";
import { closeEmulatorSettingsPanels, openEmulatorSettingsPanel, type EmulatorSettingsPanel } from "./emulator-settings";
import { restoreMultiDiscLaunch } from "./multi-disc-restore";
import { multiDiscPlayerResultCode, reportMultiDiscPlayerEvent, type MultiDiscPlayerEvent } from "./multi-disc-telemetry";
import { setEmulatorPaused } from "./pause-control";
import { captureBeforePause } from "./pause-screenshot";
import { restorePersistentSave } from "./persistent-save-restore";
import { isPspSaveFileSystem, PspPersistentSaveSync, restorePspSaveTree } from "./psp-persistent-save";
import { requiresExplicitPspStateRestore } from "./psp-state-restore";
import { PlayerChrome, type PlayerDebugRuntime } from "./player-chrome";
import { shouldRevealPlayerControls } from "./player-controls-visibility";
import { samplePlayerDebugMetrics, type PlayerDebugMetrics, type PlayerDebugSample } from "./player-debug";
import { installPlayerFrameStyle } from "./player-frame-style";
import { applyVideoRenderingMode, readVideoRenderingMode, subscribeVideoRenderingMode, writeVideoRenderingMode, type VideoRenderingMode } from "./video-rendering";
import {
  initialPlayerOrientationState,
  mobilePlayerQuery,
  observeStableOrientation,
  portraitPlayerQuery,
  reducePlayerOrientation,
  requestFullscreenAndLandscape,
  unlockLandscape,
  waitForStableLandscape,
  type PlayerOrientationEffect,
  type PlayerOrientationState,
  type PlayerRuntimeKind,
} from "./orientation";
import { NetplayController } from "./netplay/controller";
import { digestHex, EJSNetplayFrameBridge } from "./netplay/ejs-netplay-4.2.3-v1";

type ShellState = "loading" | "running" | "error";

function base64(bytes: Uint8Array) {
  let value = "";
  for (const byte of bytes) value += String.fromCharCode(byte);
  return btoa(value);
}

export async function readBoundedResponse(response: Response, maximumBytes: number, errorCode = "PLAYER_SAVE_STATE_TOO_LARGE") {
  const declared = Number(response.headers.get("content-length") ?? "0");
  if (Number.isFinite(declared) && declared > maximumBytes) throw new Error(errorCode);
  if (!response.body) {
    const bytes = new Uint8Array(await response.arrayBuffer());
    if (bytes.byteLength > maximumBytes) throw new Error(errorCode);
    return bytes;
  }
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let length = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      length += value.byteLength;
      if (length > maximumBytes) throw new Error(errorCode);
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }
  const result = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) { result.set(chunk, offset); offset += chunk.byteLength; }
  return result;
}

export function reportsNativeExit(mode: "single" | "netplay", finishing = false) {
  return mode === "single" && !finishing;
}

function formatPlayerBytes(bytes: number) {
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}

function observedRuntimeDiscCount(instance: EmulatorInstance | undefined) {
  const value = instance?.gameManager?.getDiskCount?.();
  return typeof value === "number" && Number.isInteger(value) && value >= -1 && value <= 64 ? value : null;
}

export function PlayerShell({ launchId }: { launchId: string }) {
  const { context } = useAuth();
  const userId = context.user?.userId;
  const stage = useRef<HTMLDivElement>(null);
  const frameRef = useRef<HTMLIFrameElement>(null);
  const orientationButtonRef = useRef<HTMLButtonElement>(null);
  const emulator = useRef<EmulatorInstance | undefined>(undefined);
  const [state, setState] = useState<ShellState>("loading");
  const [message, setMessage] = useState("正在验证运行快照…");
  const [warnings, setWarnings] = useState<string[]>([]);
  const [toast, setToast] = useState("");
  const [syncText, setSyncText] = useState("正在连接…");
  const [syncTone, setSyncTone] = useState<"synced" | "busy" | "warning">("busy");
  const [gameTitle, setGameTitle] = useState("正在运行的游戏");
  const [coreName, setCoreName] = useState("");
  const [platformName, setPlatformName] = useState("");
  const [controlsVisible, setControlsVisible] = useState(true);
  const [paused, setPaused] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);
  const [emulatorToolbarOpen, setEmulatorToolbarOpen] = useState(false);
  const [emulatorVolume, setEmulatorVolume] = useState(0.5);
  const [emulatorMuted, setEmulatorMuted] = useState(false);
  const videoRenderingMode = useSyncExternalStore<VideoRenderingMode>(
    subscribeVideoRenderingMode,
    () => readVideoRenderingMode(userId),
    () => "pixel",
  );
  const [discSet, setDiscSet] = useState<DiscSet | null>(null);
  const [discState, setDiscState] = useState<DiscState | null>(null);
  const [netplayPlayerNo, setNetplayPlayerNo] = useState<number | null>(null);
  const [netplayPaused, setNetplayPaused] = useState(false);
  const [debugOpen, setDebugOpen] = useState(false);
  const [frameEnabled, setFrameEnabled] = useState(false);
  const [orientationState, setOrientationState] = useState<PlayerOrientationState>(initialPlayerOrientationState);
  const [orientationHelp, setOrientationHelp] = useState("若浏览器不能自动锁定方向，请手动旋转设备。");
  const [debugMetrics, setDebugMetrics] = useState<PlayerDebugMetrics | null>(null);
  const [debugRuntime, setDebugRuntime] = useState<PlayerDebugRuntime>({
    coreId: "", coreArtifactId: "", emulatorJSVersion: "", playerAdapterId: "", inputMode: "",
    crossOriginIsolated: false, sharedArrayBuffer: false,
  });
  const returnTo = useRef("/library");
  const playerMode = useRef<PlayerConfig["mode"]>("single");
  const netplayConfig = useRef<NonNullable<PlayerConfig["netplay"]> | null>(null);
  const netplayController = useRef<NetplayController | null>(null);
  const sequence = useRef(0);
  const started = useRef(false);
  const finishing = useRef(false);
  const heartbeat = useRef<number | null>(null);
  const persistentSequence = useRef(0);
  const persistentSaveMode = useRef<PlayerConfig["persistentSaveMode"]>("SINGLE_FILE");
  const persistentQueue = useRef(Promise.resolve());
  const persistentConflict = useRef<Uint8Array | null>(null);
  const pspPersistentSync = useRef<PspPersistentSaveSync | null>(null);
  const [hasPersistentConflict, setHasPersistentConflict] = useState(false);
  const controlsTimer = useRef<number | null>(null);
  const toastTimer = useRef<number | null>(null);
  const running = useRef(false);
  const pausedRef = useRef(false);
  const pausePending = useRef(false);
  const pauseCapture = useRef<Promise<ManualScreenshot | null>>(Promise.resolve(null));
  const lastManualScreenshot = useRef<ManualScreenshot | null>(null);
  const chromePinned = useRef(false);
  const lastAudibleVolume = useRef(0.5);
  const discSetRef = useRef<DiscSet | null>(null);
  const netplayPausedRef = useRef(false);
  const orientationStateRef = useRef<PlayerOrientationState>(initialPlayerOrientationState);
  const videoRenderingModeRef = useRef<VideoRenderingMode>("pixel");

  const reportPlayerEvent = useCallback((event: MultiDiscPlayerEvent) => {
    void reportMultiDiscPlayerEvent(launchId, event).catch(() => undefined);
  }, [launchId]);

  const clearControlsTimer = useCallback(() => {
    if (controlsTimer.current !== null) window.clearTimeout(controlsTimer.current);
    controlsTimer.current = null;
  }, []);

  const showControls = useCallback(() => {
    setControlsVisible(true);
    clearControlsTimer();
    if (running.current && !pausedRef.current && !chromePinned.current) {
      controlsTimer.current = window.setTimeout(() => {
        if (!pausedRef.current && !chromePinned.current) setControlsVisible(false);
      }, 2_000);
    }
  }, [clearControlsTimer]);

  const revealControlsAtTopEdge = useCallback((clientY: number) => {
    if (shouldRevealPlayerControls(clientY)) showControls();
  }, [showControls]);

  const showToast = useCallback((value: string, timeout = 2_400) => {
    if (toastTimer.current !== null) window.clearTimeout(toastTimer.current);
    setToast(value);
    toastTimer.current = window.setTimeout(() => {
      setToast("");
      toastTimer.current = null;
    }, timeout);
  }, []);

  const holdControls = useCallback(() => {
    chromePinned.current = true;
    setControlsVisible(true);
    clearControlsTimer();
  }, [clearControlsTimer]);

  const releaseControls = useCallback(() => {
    chromePinned.current = false;
    showControls();
  }, [showControls]);

  const toggleControls = useCallback(() => {
    if (controlsVisible) {
      chromePinned.current = false;
      clearControlsTimer();
      setControlsVisible(false);
    } else {
      showControls();
    }
  }, [clearControlsTimer, controlsVisible, showControls]);

  const pauseForToolbarInteraction = useCallback(() => {
    if (playerMode.current === "netplay") return;
    if (!running.current || pausedRef.current || pausePending.current || !emulator.current) return;
    const current = emulator.current;
    pausePending.current = true;
    const capture = captureManualScreenshot(current).then((screenshot) => {
      lastManualScreenshot.current = screenshot;
      return screenshot;
    });
    pauseCapture.current = captureBeforePause(capture, () => {
      pausePending.current = false;
      if (!running.current || pausedRef.current || !setEmulatorPaused(current, true)) return;
      pausedRef.current = true;
      setPaused(true);
      showToast("游戏已暂停，点击游戏画面继续");
      setControlsVisible(true);
      clearControlsTimer();
    });
  }, [clearControlsTimer, showToast]);

  const handleGameSurfaceInteraction = useCallback(() => {
    if (playerMode.current === "netplay") return;
    if (!running.current) return;
    if (!pausedRef.current || !setEmulatorPaused(emulator.current, false)) return;
    pausedRef.current = false;
    lastManualScreenshot.current = null;
    setPaused(false);
    showToast("游戏已继续");
    showControls();
  }, [showControls, showToast]);

  const sendEvent = useCallback(async (kind: "start" | "heartbeat" | "finish") => {
    if (kind === "heartbeat" && !started.current) throw new Error("PLAY_SESSION_NOT_STARTED");
    const next = kind === "start" || kind === "finish" && !started.current ? 0 : sequence.current + 1;
    const response = await fetch(`/runtime/launches/${launchId}/${kind}`, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        clientSequence: next,
        clientObservedAtMs: Date.now(),
        previousInterval: kind === "start" || kind === "finish" && !started.current ? null : {
          running: true,
          visible: document.visibilityState === "visible",
          paused: emulator.current?.paused === true
        }
      })
    });
    if (!response.ok) throw new Error("PLAY_SESSION_EVENT_FAILED");
    sequence.current = next;
    if (kind === "start") started.current = true;
    if (kind === "finish") finishing.current = true;
  }, [launchId]);

  const uploadPersistent = useCallback((bytes: Uint8Array, event: "AUTO_INTERVAL" | "MANUAL_EXPORT" | "EXIT") => {
    const stableBytes = new Uint8Array(bytes);
    const result = persistentQueue.current.then(async () => {
      if (persistentConflict.current) return false;
      setSyncText("正在同步…");
      setSyncTone("busy");
      const digest = await sha256(stableBytes);
      const next = persistentSequence.current + 1;
      const idempotencyKey = newUuid();
      let response: Response | undefined;
      for (let attempt = 0; attempt < 2; attempt += 1) {
        try {
          response = await fetch(`/runtime/launches/${launchId}/persistent-save`, {
            method: "PUT",
            credentials: "same-origin",
            headers: {
              "Content-Type": "application/octet-stream",
              "Content-Digest": `sha-256=:${base64(digest)}:`,
              "Idempotency-Key": idempotencyKey,
              "X-Retrom-Save-Sequence": String(next),
              "X-Retrom-Save-Event": event
            },
            body: stableBytes
          });
          if (response.status < 500) break;
        } catch {
          if (attempt === 1) throw new Error("PERSISTENT_SAVE_NETWORK_FAILED");
        }
      }
      if (!response) throw new Error("PERSISTENT_SAVE_NETWORK_FAILED");
      if (!response.ok) {
        if (response.status === 409) {
          persistentConflict.current = stableBytes;
          setHasPersistentConflict(true);
          setSyncText("存档需要处理");
          setSyncTone("warning");
        } else {
          throw new Error("PERSISTENT_SAVE_FAILED");
        }
        throw new Error("PERSISTENT_SAVE_FAILED");
      }
      persistentSequence.current = next;
      setSyncText("已同步");
      setSyncTone("synced");
      return true;
    }).catch(() => {
      if (!persistentConflict.current) {
        setSyncText("同步失败");
        setSyncTone("warning");
        showToast("持久存档同步失败，最后有效版本仍被保留。", 4_000);
      }
      return false;
    });
    persistentQueue.current = result.then(() => undefined);
    return result;
  }, [launchId, showToast]);

  const uploadManualState = useCallback(async (payload: { screenshot: Blob; format: string; state: Uint8Array }) => {
    if (!payload.screenshot.size || !payload.state.byteLength) {
      setSyncText("保存失败");
      setSyncTone("warning");
      showToast("状态或截图为空，未创建存档。", 4_000);
      return false;
    }
    let discIndex: number | undefined;
    if (discSetRef.current) {
      try {
        if (!emulator.current) throw new Error("PLAYER_DISC_STATE_UNAVAILABLE");
        discIndex = readDiscState(emulator.current, discSetRef.current.count).currentIndex;
      } catch {
        setSyncText("保存失败");
        setSyncTone("warning");
        showToast("无法读取当前光盘，未创建存档。", 4_000);
        return false;
      }
    }
    const form = new FormData();
    form.append("metadata", new Blob([JSON.stringify({
      name: `手动存档 ${new Date().toLocaleString("zh-CN")}`,
      ...(discIndex === undefined ? {} : { discIndex })
    })], { type: "application/json" }));
    const stateBytes = new Uint8Array(payload.state).slice().buffer;
    form.append("state", new Blob([stateBytes], { type: "application/octet-stream" }), `state.${payload.format || "bin"}`);
    form.append("screenshot", payload.screenshot, `screenshot.${payload.format || "png"}`);
    const response = await fetch(`/runtime/launches/${launchId}/save-states`, {
      method: "POST", credentials: "same-origin",
      headers: { "Idempotency-Key": newUuid() }, body: form
    });
    if (response.ok) {
      setSyncText("已同步");
      setSyncTone("synced");
      showToast("手动存档和截图已保存");
      return true;
    } else {
      setSyncText("保存失败");
      setSyncTone("warning");
      showToast("手动存档失败，服务器未创建不完整记录", 4_000);
      return false;
    }
  }, [launchId, showToast]);

  const exit = useCallback(async () => {
    if (finishing.current) return;
    finishing.current = true;
    const exiting = reducePlayerOrientation(orientationStateRef.current, { type: "exit" });
    orientationStateRef.current = exiting.state;
    setOrientationState(exiting.state);
    if (exiting.effects.includes("unlock")) unlockLandscape();
    try {
      if (playerMode.current === "netplay") {
        netplayController.current?.end();
      } else if (persistentSaveMode.current === "FILE_TREE") {
        await pspPersistentSync.current?.flush();
        await persistentQueue.current;
      } else {
        const manager = emulator.current?.gameManager;
        const path = persistentSaveMode.current === "NONE" ? undefined : manager?.getSaveFilePath?.();
        if (persistentSaveMode.current !== "NONE" && !persistentConflict.current && path && manager?.FS?.analyzePath(path).exists) {
          const bytes = await manager.getSaveFile?.();
          if (bytes?.byteLength) void uploadPersistent(bytes, "EXIT");
        }
        await persistentQueue.current;
      }
      await sendEvent("finish");
    } catch { /* expiry is already a terminal server state */ }
    if (document.fullscreenElement) await document.exitFullscreen().catch(() => undefined);
    window.location.replace(returnTo.current);
  }, [sendEvent, uploadPersistent]);

  function downloadConflictingSave() {
    const bytes = persistentConflict.current;
    if (!bytes) return;
    const copy = new Uint8Array(bytes).slice().buffer;
    const url = URL.createObjectURL(new Blob([copy], { type: "application/octet-stream" }));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `retrom-${launchId}-local-save.bin`;
    anchor.click();
    URL.revokeObjectURL(url);
  }

  useEffect(() => {
    videoRenderingModeRef.current = videoRenderingMode;
    const canvas = emulator.current?.canvas ?? frameRef.current?.contentDocument?.querySelector<HTMLCanvasElement>("canvas") ?? null;
    applyVideoRenderingMode(emulator.current, canvas, videoRenderingMode);
  }, [videoRenderingMode]);

  useEffect(() => {
    const controller = new AbortController();
    let cleanup: (() => void) | undefined;
    let canvasContain: ReturnType<typeof installCanvasContain> | undefined;
    let cleanupFrameControls: (() => void) | undefined;
    let nativeMenuObserver: MutationObserver | undefined;
    let ownedNetplayController: NetplayController | undefined;
    async function bootstrap() {
      try {
        setMessage("正在加载 Core、ROM 与依赖配置…");
        const response = await fetch(`/runtime/launches/${launchId}/config`, { credentials: "same-origin", cache: "no-store", signal: controller.signal });
        if (!response.ok) throw new Error(`LAUNCH_CONFIG_${response.status}`);
        const config = await response.json() as PlayerConfig;
        validateConfig(config);
        returnTo.current = config.returnTo;
        playerMode.current = config.mode;
        netplayConfig.current = config.netplay;
        setNetplayPlayerNo(config.netplay?.playerNo ?? null);
        setWarnings(config.warnings ?? []);
        setGameTitle(config.gameTitle);
        setCoreName(config.coreName || config.core);
        setPlatformName(config.platformName);
        setDebugRuntime({
          coreId: config.core,
          coreArtifactId: config.coreArtifactId,
          emulatorJSVersion: config.emulatorjsVersion,
          playerAdapterId: config.playerAdapterId,
          inputMode: config.inputMode,
          crossOriginIsolated: window.crossOriginIsolated,
          sharedArrayBuffer: typeof SharedArrayBuffer !== "undefined",
        });
        persistentSaveMode.current = config.persistentSaveMode;
        discSetRef.current = config.discSet ?? null;
        setDiscSet(config.discSet ?? null);
        setDiscState(null);

        const mobileQuery = window.matchMedia(mobilePlayerQuery);
        const portraitQuery = window.matchMedia(portraitPlayerQuery);
        const runtimeKind: PlayerRuntimeKind = config.mode === "single"
          ? "single"
          : config.netplay?.playerNo === 1 ? "netplay-p1" : "netplay-p2";
        let orientation = reducePlayerOrientation(orientationStateRef.current, {
          type: "config-ready",
          mobile: mobileQuery.matches,
          portrait: portraitQuery.matches,
          runtimeKind,
        });
        orientationStateRef.current = orientation.state;
        setOrientationState(orientation.state);
        if (orientation.state.phase === "orientation-blocked") {
          setMessage("请横向握持设备开始游戏");
          await waitForStableLandscape(portraitQuery, controller.signal, (portrait) => {
            orientation = reducePlayerOrientation(orientationStateRef.current, {
              type: "orientation-stable", portrait, paused: false,
            });
            orientationStateRef.current = orientation.state;
            setOrientationState(orientation.state);
          });
        }

        setFrameEnabled(true);
        for (let attempt = 0; attempt < 12 && !frameRef.current; attempt += 1) {
          await new Promise<void>((resolve) => window.requestAnimationFrame(() => resolve()));
          if (controller.signal.aborted) return;
        }
        if (!frameRef.current) throw new Error("PLAYER_FRAME_UNAVAILABLE");

        if (config.discSet) {
          const sizes = await Promise.all(config.discSet.entries.map(async (entry) => {
            const source = config.externalFiles[entry.virtualPath];
            const head = await fetch(source, { method: "HEAD", credentials: "same-origin", cache: "no-store", signal: controller.signal });
            if (!head.ok) throw new Error("PLAYER_DISC_SET_INVALID");
            const size = Number(head.headers.get("content-length") ?? "NaN");
            if (!Number.isSafeInteger(size) || size < 8) throw new Error("PLAYER_DISC_SET_INVALID");
            return size;
          }));
          setMessage(`正在准备多盘内容 · ${config.discSet.count} 张光盘 · ${formatPlayerBytes(sizes.reduce((total, size) => total + size, 0))}`);
        }

        let persistentBytes: Uint8Array | null = null;
        if (config.persistentSaveMode === "NONE") {
          if (config.persistentSaveUrl !== null) throw new Error("PLAYER_PERSISTENT_CAPABILITY_INVALID");
          setSyncText("仅支持状态存档");
          setSyncTone("warning");
        } else {
          if (!config.persistentSaveUrl) throw new Error("PLAYER_PERSISTENT_CAPABILITY_INVALID");
          const persistentResponse = await fetch(config.persistentSaveUrl, { credentials: "same-origin", cache: "no-store", signal: controller.signal });
          if (!persistentResponse.ok && persistentResponse.status !== 204) throw new Error("LAUNCH_PERSISTENT_SAVE_LOAD_FAILED");
          persistentBytes = persistentResponse.status === 204
            ? null
            : await readBoundedResponse(persistentResponse, 64 * 1024 * 1024, "LAUNCH_PERSISTENT_SAVE_TOO_LARGE");
        }

        let stateBytes: Uint8Array | null = null;
        if ((config.discSet || requiresExplicitPspStateRestore(config)) && config.stateUrl) {
          const stateResponse = await fetch(config.stateUrl, { credentials: "same-origin", cache: "no-store", signal: controller.signal });
          if (!stateResponse.ok) throw new Error("PLAYER_SAVE_STATE_UNAVAILABLE");
          stateBytes = await readBoundedResponse(stateResponse, 64 * 1024 * 1024);
          if (stateBytes.byteLength === 0) throw new Error("PLAYER_SAVE_STATE_UNAVAILABLE");
        }

        if (!stage.current || !frameRef.current) return;
        const frame = frameRef.current;
        const frameWindow = frame.contentWindow;
        const frameDocument = frame.contentDocument;
        if (!frameWindow || !frameDocument) throw new Error("PLAYER_FRAME_UNAVAILABLE");
        frameDocument.documentElement.lang = "zh-CN";
        frameDocument.documentElement.classList.add("retrom-native-menu-locked");
        installPlayerFrameStyle(frameDocument);
        const target = frameDocument.createElement("div");
        target.id = "game";
        frameDocument.body.append(target);
        canvasContain = installCanvasContain(frameDocument, () => emulator.current?.gameManager?.getVideoDimensions?.("aspect"));
        const handleFramePointerMove = (event: PointerEvent) => revealControlsAtTopEdge(event.clientY);
        const handleFrameKeyDown = () => showControls();
        const handleFrameClick = (event: MouseEvent) => {
          const target = event.target;
          if (target && "closest" in target && typeof target.closest === "function" && target.closest(".ejs_menu_bar,.ejs_popup_container,.ejs_cheat_parent,.ejs_control_bar,button,a,input,select,textarea,[role=button]")) return;
          frame.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true }));
          frame.dispatchEvent(new MouseEvent("click", { bubbles: true }));
        };
        frameDocument.addEventListener("pointermove", handleFramePointerMove, { passive: true });
        frameDocument.addEventListener("keydown", handleFrameKeyDown);
        if (config.inputMode === "STANDARD") frameDocument.addEventListener("click", handleFrameClick);
        cleanupFrameControls = () => {
          frameDocument.removeEventListener("pointermove", handleFramePointerMove);
          frameDocument.removeEventListener("keydown", handleFrameKeyDown);
          if (config.inputMode === "STANDARD") frameDocument.removeEventListener("click", handleFrameClick);
        };

        let mountedSaveFS: NonNullable<NonNullable<EmulatorInstance["gameManager"]>["FS"]> | undefined;
        cleanup = mountEmulatorJS(config, target, {
          onReady: (instance) => {
            if (controller.signal.aborted) return;
            emulator.current = instance;
            const runningCanvas = instance.canvas ?? frameDocument.querySelector<HTMLCanvasElement>("canvas");
            applyVideoRenderingMode(instance, runningCanvas, videoRenderingModeRef.current);
            const initialVolume = Math.min(1, Math.max(0, typeof instance.volume === "number" ? instance.volume : 0.5));
            setEmulatorVolume(initialVolume);
            setEmulatorMuted(instance.muted === true || initialVolume === 0);
            if (initialVolume > 0) lastAudibleVolume.current = initialVolume;
            const nativeMenu = frameDocument.querySelector<HTMLElement>(".ejs_menu_bar");
            if (nativeMenu) {
              nativeMenuObserver = new MutationObserver(() => {
                if (nativeMenu.classList.contains("ejs_menu_bar_hidden")) {
                  frameDocument.documentElement.classList.add("retrom-native-menu-locked");
                }
              });
              nativeMenuObserver.observe(nativeMenu, { attributes: true, attributeFilter: ["class"] });
            }
            instance.on("saveState", () => undefined);
            if (config.persistentSaveMode !== "NONE") {
              instance.on("saveDatabaseLoaded", () => {
                const fs = instance.gameManager?.FS;
                if (!fs) {
                  setState("error");
                  setMessage("LAUNCH_PERSISTENT_SAVE_FS_UNAVAILABLE");
                  throw new Error("LAUNCH_PERSISTENT_SAVE_FS_UNAVAILABLE");
                }
                mountedSaveFS = fs;
                if (config.persistentSaveMode === "FILE_TREE") {
                  if (!isPspSaveFileSystem(fs) || !instance.gameManager) {
                    setState("error");
                    setMessage("LAUNCH_PERSISTENT_SAVE_FS_UNAVAILABLE");
                    throw new Error("LAUNCH_PERSISTENT_SAVE_FS_UNAVAILABLE");
                  }
                  try {
                    restorePspSaveTree(fs, persistentBytes);
                    const sync = new PspPersistentSaveSync(fs, instance.gameManager, uploadPersistent, {
                      isPaused: () => emulator.current?.paused === true,
                      onError: (error) => {
                        setSyncText("同步失败");
                        setSyncTone("warning");
                        showToast(error.message === "LAUNCH_PERSISTENT_SAVE_TOO_LARGE"
                          ? "PSP 游戏内存档超过 64 MiB，未覆盖服务器上的最后有效版本。"
                          : "无法读取 PSP 游戏内存档，服务器上的最后有效版本仍被保留。", 4_000);
                      },
                    });
                    pspPersistentSync.current?.stop();
                    pspPersistentSync.current = sync;
                  } catch (error) {
                    setState("error");
                    setMessage(error instanceof Error ? error.message : "LAUNCH_PERSISTENT_SAVE_LOAD_FAILED");
                    throw error;
                  }
                }
              });
              if (config.persistentSaveMode !== "FILE_TREE") {
                instance.on("saveSaveFiles", (value: unknown) => {
                  if (value instanceof Uint8Array && value.byteLength) void uploadPersistent(value, "AUTO_INTERVAL");
                });
              }
            }
            instance.on("exit", () => {
              if (!reportsNativeExit(playerMode.current, finishing.current)) return;
              void sendEvent("finish").catch(() => {
                setState("error");
                setMessage("PLAY_SESSION_EVENT_FAILED");
              });
            });
          },
          onGameStart: () => {
            if (controller.signal.aborted) return false;
            frameDocument.documentElement.classList.add("retrom-native-menu-locked");
            emulator.current?.menu?.close?.();
            const manager = emulator.current?.gameManager;
            const runningCanvas = emulator.current?.canvas ?? frameDocument.querySelector<HTMLCanvasElement>("canvas");
            applyVideoRenderingMode(emulator.current, runningCanvas, videoRenderingModeRef.current);
            const completeSinglePlayerStart = (resumeMainLoop: boolean) => {
              if (controller.signal.aborted) return;
              if (emulator.current) {
                emulator.current.paused = false;
                if (resumeMainLoop) emulator.current.gameManager?.toggleMainLoop?.(true);
              }
              pausedRef.current = false;
              setPaused(false);
              if (config.persistentSaveMode === "FILE_TREE") pspPersistentSync.current?.start();
              const startedOrientation = reducePlayerOrientation(orientationStateRef.current, { type: "runtime-started", paused: false });
              orientationStateRef.current = startedOrientation.state;
              setOrientationState(startedOrientation.state);
              frameWindow.requestAnimationFrame(() => canvasContain?.refresh());
              void sendEvent("start").then(() => {
                setState("running");
                setSyncText(config.persistentSaveMode === "NONE" ? "仅支持状态存档" : "已同步");
                setSyncTone(config.persistentSaveMode === "NONE" ? "warning" : "synced");
                heartbeat.current = window.setInterval(() => { void sendEvent("heartbeat"); }, 30_000);
              }).catch(() => {
                setState("error");
                setMessage("PLAY_SESSION_EVENT_FAILED");
              });
            };
            try {
              if (config.discSet) {
                let persistentRestore = null;
                if (config.persistentSaveMode !== "NONE" && config.persistentSaveMode !== "FILE_TREE") {
                  const savePath = manager?.getSaveFilePath?.();
                  if (!mountedSaveFS || !savePath) throw new Error("LAUNCH_PERSISTENT_SAVE_LOAD_FAILED");
                  persistentRestore = { fileSystem: mountedSaveFS, savePath, bytes: persistentBytes };
                }
                setMessage(`正在切换到光盘 ${config.discSet.initialDiscIndex + 1}`);
                if (!emulator.current) throw new Error("PLAYER_DISC_API_UNAVAILABLE");
                const selected = restoreMultiDiscLaunch(emulator.current, config.discSet, persistentRestore, stateBytes);
                setDiscState(selected);
                reportPlayerEvent({
                  eventType: "START", resultCode: "OK", discCount: config.discSet.count,
                  observedDiscCount: selected.count,
                });
                if (stateBytes) reportPlayerEvent({
                  eventType: "SAVE_RESTORE_SUCCESS", resultCode: "OK", discCount: config.discSet.count,
                  observedDiscCount: selected.count,
                });
              } else if (config.persistentSaveMode === "FILE_TREE") {
                if (!pspPersistentSync.current) throw new Error("LAUNCH_PERSISTENT_SAVE_LOAD_FAILED");
                if (stateBytes) {
                  if (!manager?.loadPspStateAndWait) throw new Error("PSP_STATE_RESTORE_COMPATIBILITY_UNAVAILABLE");
                  setMessage("正在恢复 PSP 状态存档");
                  void manager.loadPspStateAndWait(stateBytes).then(() => {
                    completeSinglePlayerStart(true);
                  }).catch(() => {
                    if (controller.signal.aborted) return;
                    setState("error");
                    setMessage("PLAYER_SAVE_STATE_RESTORE_FAILED");
                  });
                  return false;
                }
              } else if (config.persistentSaveMode !== "NONE") {
                const savePath = manager?.getSaveFilePath?.();
                if (!manager || !mountedSaveFS || !savePath) throw new Error("LAUNCH_PERSISTENT_SAVE_LOAD_FAILED");
                restorePersistentSave(manager, mountedSaveFS, savePath, persistentBytes);
              }
            } catch (caught) {
              if (config.discSet) {
                const observedDiscCount = observedRuntimeDiscCount(emulator.current);
                const resultCode = multiDiscPlayerResultCode(
                  caught, stateBytes ? "PLAYER_SAVE_STATE_RESTORE_FAILED" : "PLAYER_DISC_API_UNAVAILABLE",
                );
                if (resultCode === "PLAYER_DISC_SET_INVALID" && observedDiscCount !== null &&
                  observedDiscCount !== config.discSet.count) {
                  reportPlayerEvent({
                    eventType: "DISK_COUNT_MISMATCH", resultCode, discCount: config.discSet.count,
                    observedDiscCount,
                  });
                }
                if (stateBytes) reportPlayerEvent({
                  eventType: "SAVE_RESTORE_FAILURE", resultCode, discCount: config.discSet.count,
                  observedDiscCount,
                });
              }
              setState("error");
              setMessage(caught instanceof Error ? caught.message : "PLAYER_DISC_SET_INVALID");
              return false;
            }
            if (config.mode === "netplay") {
              if (!emulator.current || !config.netplay) throw new Error("PLAYER_NETPLAY_CONFIG_INVALID");
              try {
                netplayController.current?.dispose();
                const bridge = new EJSNetplayFrameBridge(emulator.current);
                const controllerHolder: { current?: NetplayController } = {};
                const isCurrent = () => !controller.signal.aborted && netplayController.current === controllerHolder.current;
                const createdController = new NetplayController(config.netplay, "", bridge, {
                  onStatus: (text, tone) => {
                    if (!isCurrent()) return;
                    setSyncText(text); setSyncTone(tone);
                  },
                  onRunning: () => {
                    if (!isCurrent()) return;
                    setNetplayPaused(false);
                    netplayPausedRef.current = false;
                    const startedOrientation = reducePlayerOrientation(orientationStateRef.current, { type: "runtime-started", paused: false });
                    orientationStateRef.current = startedOrientation.state;
                    setOrientationState(startedOrientation.state);
                    if (started.current) return;
                    void sendEvent("start").then(() => {
                      setState("running");
                      heartbeat.current = window.setInterval(() => { void sendEvent("heartbeat"); }, 30_000);
                    }).catch(() => {
                      setState("error");
                      setMessage("PLAY_SESSION_EVENT_FAILED");
                    });
                  },
                  onPaused: () => { if (isCurrent()) { netplayPausedRef.current = true; setNetplayPaused(true); } },
                  onEnded: (reason) => {
                    if (!isCurrent()) return;
                    setSyncText("联机已结束");
                    setSyncTone("warning");
                    setMessage(reason);
                    void sendEvent("finish").catch(() => undefined).finally(() => {
                      window.setTimeout(() => window.location.replace(returnTo.current), 600);
                    });
                  },
                });
                controllerHolder.current = createdController;
                ownedNetplayController = createdController;
                netplayController.current = createdController;
                setMessage("正在建立联机同步屏障…");
                void digestHex(new TextEncoder().encode(JSON.stringify(config.netplay.netplayProfile)))
                  .then((profileDigest) => createdController.setProfileDigest(profileDigest))
                  .then(() => createdController.start())
                  .catch((caught: unknown) => {
                    if (!isCurrent()) { createdController.dispose(); return; }
                    createdController.end();
                    setState("error");
                    setMessage(caught instanceof Error ? caught.message : "NETPLAY_START_FAILED");
                  });
                return true;
              } catch (caught) {
                setState("error");
                setMessage(caught instanceof Error ? caught.message : "NETPLAY_START_FAILED");
                return false;
              }
            }
            completeSinglePlayerStart(false);
            return true;
          },
          onSaveState: (payload) => { void uploadManualState(payload); },
          onSaveSave: config.persistentSaveMode === "SINGLE_FILE" || config.persistentSaveMode === "DOS_OVERLAY"
            ? (payload) => { if (payload.save.byteLength) void uploadPersistent(payload.save, "MANUAL_EXPORT"); }
            : undefined
        }, frameWindow);
      } catch (error) {
        if (controller.signal.aborted) return;
        const code = error instanceof Error ? error.message : "启动失败";
        setMessage(code === "LAUNCH_CONFIG_401" ? "启动会话不可用，请从游戏详情或存档重新开始。" : code);
        setState("error");
      }
    }
    void bootstrap();
    return () => {
      controller.abort(); cleanup?.(); canvasContain?.cleanup(); cleanupFrameControls?.();
      ownedNetplayController?.dispose();
      if (netplayController.current === ownedNetplayController) netplayController.current = null;
      closeEmulatorSettingsPanels(emulator.current);
      nativeMenuObserver?.disconnect();
      pspPersistentSync.current?.stop();
      pspPersistentSync.current = null;
      if (heartbeat.current !== null) window.clearInterval(heartbeat.current);
      if (toastTimer.current !== null) window.clearTimeout(toastTimer.current);
    };
  }, [exit, launchId, reportPlayerEvent, revealControlsAtTopEdge, sendEvent, showControls, showToast, uploadManualState, uploadPersistent]);

  useEffect(() => {
    running.current = state === "running";
    clearControlsTimer();
    if (running.current && !pausedRef.current && !chromePinned.current) controlsTimer.current = window.setTimeout(() => setControlsVisible(false), 2_000);
    return clearControlsTimer;
  }, [clearControlsTimer, state]);

  useEffect(() => {
    const updateFullscreen = () => setFullscreen(document.fullscreenElement !== null);
    updateFullscreen();
    document.addEventListener("fullscreenchange", updateFullscreen);
    return () => document.removeEventListener("fullscreenchange", updateFullscreen);
  }, []);

  useEffect(() => {
    if (orientationState.phase !== "orientation-blocked") return;
    const frame = requestAnimationFrame(() => orientationButtonRef.current?.focus());
    return () => cancelAnimationFrame(frame);
  }, [orientationState.phase]);

  useEffect(() => {
    if (!debugOpen) return;
    let previous: PlayerDebugSample | null = null;
    const sample = () => {
      const canvas = emulator.current?.canvas ?? frameRef.current?.contentDocument?.querySelector("canvas") ?? null;
      const result = samplePlayerDebugMetrics(emulator.current, canvas, previous, performance.now(), {
        width: window.innerWidth,
        height: window.innerHeight,
        devicePixelRatio: window.devicePixelRatio,
      });
      previous = result.sample;
      setDebugMetrics(result.metrics);
    };
    const initialFrame = window.requestAnimationFrame(sample);
    const timer = window.setInterval(sample, 1_000);
    window.addEventListener("resize", sample);
    return () => {
      window.cancelAnimationFrame(initialFrame);
      window.clearInterval(timer);
      window.removeEventListener("resize", sample);
    };
  }, [debugOpen]);

  function toggleDebug() {
    if (!debugOpen) setDebugMetrics(null);
    setDebugOpen((open) => !open);
  }

  useEffect(() => {
    const handlePageHide = () => {
      if (finishing.current) return;
      const wasStarted = started.current;
      const next = wasStarted ? sequence.current + 1 : 0;
      void fetch(`/runtime/launches/${launchId}/finish`, {
        method: "POST",
        credentials: "same-origin",
        keepalive: true,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          clientSequence: next,
          clientObservedAtMs: Date.now(),
          previousInterval: wasStarted ? { running: true, visible: document.visibilityState === "visible", paused: emulator.current?.paused === true } : null
        })
      });
      finishing.current = true;
    };
    window.addEventListener("pagehide", handlePageHide);
    return () => window.removeEventListener("pagehide", handlePageHide);
  }, [launchId]);

  useEffect(() => {
    const releaseHiddenControls = () => {
      if (document.visibilityState === "hidden" && playerMode.current === "netplay") netplayController.current?.handleFocusLoss();
    };
    const releaseBlurredControls = () => {
      if (playerMode.current === "netplay") netplayController.current?.handleFocusLoss();
    };
    document.addEventListener("visibilitychange", releaseHiddenControls);
    window.addEventListener("blur", releaseBlurredControls);
    return () => {
      document.removeEventListener("visibilitychange", releaseHiddenControls);
      window.removeEventListener("blur", releaseBlurredControls);
    };
  }, []);

  async function saveManualState() {
    const current = emulator.current;
    if (!current) return false;
    setSyncText("正在保存…");
    setSyncTone("busy");
    showToast("正在创建存档…");
    try {
      const capture = await pauseCapture.current ?? lastManualScreenshot.current;
      if (!capture) throw new Error("PLAYER_SCREENSHOT_UNAVAILABLE");
      return await uploadManualState(captureManualState(current, capture));
    } catch {
      setSyncText("保存失败");
      setSyncTone("warning");
      showToast("无法从模拟器读取完整状态和截图", 4_000);
      return false;
    }
  }

  async function selectDisc(index: number) {
    const current = emulator.current;
    const locked = discSetRef.current;
    if (!current || !locked || !discState) return false;
    if (index === discState.currentIndex) return true;
    try {
      const selected = switchDiscPreservingPause(current, index, locked.count);
      setDiscState(selected);
      reportPlayerEvent({
        eventType: "SWITCH_SUCCESS", resultCode: "OK", discCount: locked.count,
        observedDiscCount: selected.count,
      });
      showToast(`已切换到光盘 ${selected.currentIndex + 1}`);
      return true;
    } catch (caught) {
      reportPlayerEvent({
        eventType: "SWITCH_FAILURE",
        resultCode: multiDiscPlayerResultCode(caught, "PLAYER_DISC_SWITCH_FAILED"),
        discCount: locked.count,
        observedDiscCount: observedRuntimeDiscCount(current),
      });
      showToast(`无法切换光盘，游戏仍停留在光盘 ${discState.currentIndex + 1}`, 4_000);
      return false;
    }
  }

  async function toggleFullscreen() {
    if (document.fullscreenElement) await document.exitFullscreen().catch(() => showToast("浏览器未能退出全屏"));
    else await document.documentElement.requestFullscreen({ navigationUI: "hide" }).catch(() => showToast("浏览器未允许全屏，游戏仍会继续运行。", 4_000));
  }

  function openEmulatorSettings() {
    if (!emulator.current || state !== "running") {
      showToast("模拟器设置尚未准备好，请稍后再试。", 3_000);
      return;
    }
    setEmulatorToolbarOpen(true);
    holdControls();
  }

  function closeEmulatorSettings() {
    closeEmulatorSettingsPanels(emulator.current);
    setEmulatorToolbarOpen(false);
    releaseControls();
  }

  function openEmulatorPanel(panel: EmulatorSettingsPanel) {
    const current = emulator.current;
    if (!current || !openEmulatorSettingsPanel(current, panel)) {
      showToast("当前模拟器未提供这项设置。", 3_000);
      return;
    }
    holdControls();
  }

  function changeEmulatorVolume(volume: number) {
    const current = emulator.current;
    if (!current) return;
    const normalized = Math.min(1, Math.max(0, volume));
    current.volume = normalized;
    current.muted = normalized === 0;
    current.setVolume?.(normalized);
    setEmulatorVolume(normalized);
    setEmulatorMuted(normalized === 0);
    if (normalized > 0) lastAudibleVolume.current = normalized;
  }

  function toggleEmulatorMute() {
    const current = emulator.current;
    if (!current) return;
    if (emulatorMuted) {
      const restored = Math.min(1, Math.max(0.01, lastAudibleVolume.current));
      current.volume = restored;
      current.muted = false;
      current.setVolume?.(restored);
      setEmulatorVolume(restored);
      setEmulatorMuted(false);
      return;
    }
    if (emulatorVolume > 0) lastAudibleVolume.current = emulatorVolume;
    current.muted = true;
    current.setVolume?.(0);
    setEmulatorMuted(true);
  }

  function changeVideoRenderingMode(mode: VideoRenderingMode) {
    videoRenderingModeRef.current = mode;
    writeVideoRenderingMode(userId, mode);
    const canvas = emulator.current?.canvas ?? frameRef.current?.contentDocument?.querySelector<HTMLCanvasElement>("canvas") ?? null;
    const runtimeApplied = applyVideoRenderingMode(emulator.current, canvas, mode);
    showToast(runtimeApplied ? "画面模式已应用" : "画面模式将在模拟器准备完成后应用");
  }

  async function toggleNetplayPause() {
    const locked = netplayConfig.current;
    if (!locked || locked.playerNo !== 1) return;
    const action = netplayPaused ? "resume" : "pause";
    const response = await fetch(`/api/v1/netplay/rooms/${locked.roomId}/sessions/${locked.sessionId}/${action}`, {
      method: "POST",
      credentials: "same-origin",
      headers: writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }),
      body: "{}",
    });
    if (!response.ok) {
      showToast("无法更改全局暂停状态，请重试。", 4_000);
      return;
    }
    if (!netplayPaused) {
      netplayPausedRef.current = true;
      setNetplayPaused(true);
    }
  }

  const requestNetplayPause = useCallback(async (action: "pause" | "resume") => {
    const locked = netplayConfig.current;
    if (!locked || locked.playerNo !== 1) return false;
    const response = await fetch(`/api/v1/netplay/rooms/${locked.roomId}/sessions/${locked.sessionId}/${action}`, {
      method: "POST",
      credentials: "same-origin",
      headers: writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }),
      body: "{}",
    }).catch(() => null);
    if (!response?.ok) return false;
    netplayPausedRef.current = action === "pause";
    setNetplayPaused(action === "pause");
    return true;
  }, []);

  const runOrientationEffects = useCallback(async (effects: PlayerOrientationEffect[]) => {
    const queue = [...effects];
    while (queue.length) {
      const effect = queue.shift();
      if (effect === "release-input") {
        frameRef.current?.blur();
        frameRef.current?.contentWindow?.dispatchEvent(new Event("blur"));
        if (playerMode.current === "netplay") netplayController.current?.handleFocusLoss();
      } else if (effect === "pause-single") {
        if (setEmulatorPaused(emulator.current, true)) {
          pausedRef.current = true;
          setPaused(true);
        }
      } else if (effect === "resume-single") {
        if (document.visibilityState === "visible" && setEmulatorPaused(emulator.current, false)) {
          pausedRef.current = false;
          setPaused(false);
          showControls();
        }
      } else if (effect === "pause-netplay") {
        const owned = await requestNetplayPause("pause");
        if (!owned) {
          showToast("无法在旋转时暂停联机，请立即横屏并手动确认状态。", 4_000);
          continue;
        }
        const transition = reducePlayerOrientation(orientationStateRef.current, { type: "netplay-pause-owned" });
        orientationStateRef.current = transition.state;
        setOrientationState(transition.state);
        queue.unshift(...transition.effects);
      } else if (effect === "resume-netplay") {
        if (!await requestNetplayPause("resume")) showToast("无法自动恢复联机，请由房主手动继续。", 4_000);
      } else if (effect === "warn-netplay-p2") {
        showToast("本局仍在进行，请立即横屏。", 4_000);
      }
    }
  }, [requestNetplayPause, showControls, showToast]);

  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const portraitQuery = window.matchMedia(portraitPlayerQuery);
    const apply = (portrait: boolean) => {
      const pausedNow = playerMode.current === "single" ? pausedRef.current : netplayPausedRef.current;
      const transition = reducePlayerOrientation(orientationStateRef.current, { type: "orientation-stable", portrait, paused: pausedNow });
      orientationStateRef.current = transition.state;
      setOrientationState(transition.state);
      void runOrientationEffects(transition.effects);
    };
    return observeStableOrientation(portraitQuery, apply);
  }, [runOrientationEffects]);

  useEffect(() => {
    const updateVisibility = () => {
      const transition = reducePlayerOrientation(orientationStateRef.current, { type: "visibility", hidden: document.visibilityState === "hidden" });
      orientationStateRef.current = transition.state;
      setOrientationState(transition.state);
      void runOrientationEffects(transition.effects);
    };
    document.addEventListener("visibilitychange", updateVisibility);
    return () => document.removeEventListener("visibilitychange", updateVisibility);
  }, [runOrientationEffects]);

  async function retryLandscape() {
    const result = await requestFullscreenAndLandscape();
    if (result.orientation === "unsupported") setOrientationHelp("当前浏览器不支持自动锁定方向，请手动旋转设备。");
    else if (result.orientation === "denied") setOrientationHelp("浏览器拒绝了方向锁定，请手动旋转设备。");
    else setOrientationHelp("方向已锁定；若画面没有变化，请手动旋转设备。");
  }

  return (
    <main className={`player-shell${paused ? " is-paused" : ""}${orientationState.phase === "orientation-blocked" ? " is-orientation-blocked" : ""}`} onKeyDown={showControls} onPointerMove={(event) => revealControlsAtTopEdge(event.clientY)}>
      {orientationState.phase !== "orientation-blocked" ? <PlayerChrome
        controlsVisible={controlsVisible}
        running={state === "running"}
        paused={paused}
        fullscreen={fullscreen}
        gameTitle={gameTitle}
        coreName={coreName}
        platformName={platformName}
        syncText={syncText}
        syncTone={syncTone}
        toast={toast}
        warnings={warnings}
        hasPersistentConflict={hasPersistentConflict}
        emulatorToolbarOpen={emulatorToolbarOpen}
        emulatorVolume={emulatorVolume}
        emulatorMuted={emulatorMuted}
        videoRenderingMode={videoRenderingMode}
        discSet={discSet}
        discState={discState}
        netplayPlayerNo={netplayPlayerNo}
        netplayPaused={netplayPaused}
        debugOpen={debugOpen}
        debugMetrics={debugMetrics}
        debugRuntime={debugRuntime}
        runtimeState={state}
        onHoldControls={holdControls}
        onReleaseControls={releaseControls}
        onToggleControls={toggleControls}
        onSave={saveManualState}
        onPauseForToolbarInteraction={pauseForToolbarInteraction}
        onToggleFullscreen={() => void toggleFullscreen()}
        onOpenEmulatorSettings={openEmulatorSettings}
        onCloseEmulatorSettings={closeEmulatorSettings}
        onOpenEmulatorPanel={openEmulatorPanel}
        onChangeEmulatorVolume={changeEmulatorVolume}
        onToggleEmulatorMute={toggleEmulatorMute}
        onChangeVideoRenderingMode={changeVideoRenderingMode}
        onSelectDisc={selectDisc}
        onToggleNetplayPause={() => void toggleNetplayPause()}
        onToggleDebug={toggleDebug}
        onExit={() => void exit()}
        onDownloadConflict={downloadConflictingSave}
      /> : null}
      <div className="player-stage" ref={stage} inert={orientationState.phase === "orientation-blocked" ? true : undefined} aria-hidden={orientationState.phase === "orientation-blocked" || undefined} onClick={handleGameSurfaceInteraction}>
        {frameEnabled ? <iframe ref={frameRef} title="Retrom EmulatorJS Player" className="player-frame" src="about:blank" /> : null}
        {state !== "running" ? <div className="player-loading">{state === "loading" ? <i /> : null}<strong>{message}</strong><p>{state === "error" ? <><span>凭据可能已过期或依赖不兼容。</span> <Link href="/library">返回游戏库</Link></> : "页面会在验证和存档预取后自动开始，无需再次点击。"}</p></div> : null}
      </div>
      {orientationState.phase === "orientation-blocked" ? <section className="player-orientation-gate" role="dialog" aria-modal="true" aria-labelledby="player-orientation-title" onKeyDown={(event) => { if (event.key === "Tab") { event.preventDefault(); orientationButtonRef.current?.focus(); } }}>
        <div className="player-rotate-mark" aria-hidden="true"><span>↻</span></div>
        <p>{orientationState.runtimeKind === "netplay-p2" && orientationState.started ? "联机仍在进行" : orientationState.started ? "游戏已暂停" : "移动 Player 需要横屏"}</p>
        <h1 id="player-orientation-title">请横向握持设备开始游戏</h1>
        <strong>{gameTitle}</strong>
        <small>{orientationState.runtimeKind === "netplay-p2" && orientationState.started ? "你是 P2，不能暂停全局联机；本地输入已清空。" : orientationHelp}</small>
        <button ref={orientationButtonRef} className="button" type="button" onClick={() => void retryLandscape()}>尝试进入全屏并横屏</button>
      </section> : null}
    </main>
  );
}
