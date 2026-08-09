"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { newUuid, sha256 } from "@/lib/crypto";
import { captureManualScreenshot, captureManualState, mountEmulatorJS, type EmulatorInstance, type ManualScreenshot, type PlayerConfig } from "./adapters/ejs-4.2.3-v1";
import { installCanvasContain } from "./canvas-fit";
import { closeEmulatorSettingsPanels, openEmulatorSettingsPanel, type EmulatorSettingsPanel } from "./emulator-settings";
import { setEmulatorPaused } from "./pause-control";
import { restorePersistentSave } from "./persistent-save-restore";
import { PlayerChrome } from "./player-chrome";
import { shouldRevealPlayerControls } from "./player-controls-visibility";

type ShellState = "loading" | "running" | "error";

function base64(bytes: Uint8Array) {
  let value = "";
  for (const byte of bytes) value += String.fromCharCode(byte);
  return btoa(value);
}

export function PlayerShell({ launchId }: { launchId: string }) {
  const stage = useRef<HTMLDivElement>(null);
  const frameRef = useRef<HTMLIFrameElement>(null);
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
  const returnTo = useRef("/library");
  const sequence = useRef(0);
  const started = useRef(false);
  const finishing = useRef(false);
  const heartbeat = useRef<number | null>(null);
  const persistentSequence = useRef(0);
  const persistentSaveMode = useRef<PlayerConfig["persistentSaveMode"]>("SINGLE_FILE");
  const persistentQueue = useRef(Promise.resolve());
  const persistentConflict = useRef<Uint8Array | null>(null);
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

  const pauseForToolbarInteraction = useCallback(() => {
    if (!running.current || pausedRef.current || pausePending.current || !emulator.current) return;
    const current = emulator.current;
    pausePending.current = true;
    let timeoutID: number | undefined;
    const timeout = new Promise<null>((resolve) => {
      timeoutID = window.setTimeout(() => resolve(null), 750);
    });
    pauseCapture.current = Promise.race([captureManualScreenshot(current), timeout])
      .then((capture) => {
        if (capture) lastManualScreenshot.current = capture;
        return capture;
      })
      .catch(() => null)
      .finally(() => {
        if (timeoutID !== undefined) window.clearTimeout(timeoutID);
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
    persistentQueue.current = persistentQueue.current.then(async () => {
      if (persistentConflict.current) return;
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
          setSyncText("同步失败");
          setSyncTone("warning");
          showToast("持久存档同步失败，最后有效版本仍被保留。", 4_000);
        }
        throw new Error("PERSISTENT_SAVE_FAILED");
      }
      persistentSequence.current = next;
      setSyncText("已同步");
      setSyncTone("synced");
    }).catch(() => undefined);
  }, [launchId, showToast]);

  const uploadManualState = useCallback(async (payload: { screenshot: Blob; format: string; state: Uint8Array }) => {
    if (!payload.screenshot.size || !payload.state.byteLength) {
      setSyncText("保存失败");
      setSyncTone("warning");
      showToast("状态或截图为空，未创建存档。", 4_000);
      return false;
    }
    const form = new FormData();
    form.append("metadata", new Blob([JSON.stringify({ name: `手动存档 ${new Date().toLocaleString("zh-CN")}` })], { type: "application/json" }));
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
    try {
      const manager = emulator.current?.gameManager;
      const path = persistentSaveMode.current === "NONE" ? undefined : manager?.getSaveFilePath?.();
      if (persistentSaveMode.current !== "NONE" && !persistentConflict.current && path && manager?.FS?.analyzePath(path).exists) {
        const bytes = await manager.getSaveFile?.();
        if (bytes?.byteLength) uploadPersistent(bytes, "EXIT");
      }
      await persistentQueue.current;
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
    const controller = new AbortController();
    let cleanup: (() => void) | undefined;
    let canvasContain: ReturnType<typeof installCanvasContain> | undefined;
    let cleanupFrameControls: (() => void) | undefined;
    let nativeMenuObserver: MutationObserver | undefined;
    async function bootstrap() {
      try {
        setMessage("正在加载 Core、ROM 与依赖配置…");
        const response = await fetch(`/runtime/launches/${launchId}/config`, { credentials: "same-origin", cache: "no-store", signal: controller.signal });
        if (!response.ok) throw new Error(`LAUNCH_CONFIG_${response.status}`);
        const config = await response.json() as PlayerConfig;
        returnTo.current = config.returnTo;
        setWarnings(config.warnings ?? []);
        setGameTitle(config.gameTitle);
        setCoreName(config.coreName || config.core);
        setPlatformName(config.platformName);
        persistentSaveMode.current = config.persistentSaveMode;

        let persistentBytes: Uint8Array | null = null;
        if (config.persistentSaveMode === "NONE") {
          if (config.persistentSaveUrl !== null) throw new Error("PLAYER_PERSISTENT_CAPABILITY_INVALID");
          setSyncText("仅支持状态存档");
          setSyncTone("warning");
        } else {
          if (!config.persistentSaveUrl) throw new Error("PLAYER_PERSISTENT_CAPABILITY_INVALID");
          const persistentResponse = await fetch(config.persistentSaveUrl, { credentials: "same-origin", cache: "no-store", signal: controller.signal });
          if (!persistentResponse.ok && persistentResponse.status !== 204) throw new Error("LAUNCH_PERSISTENT_SAVE_LOAD_FAILED");
          const contentLength = Number(persistentResponse.headers.get("content-length") ?? "0");
          if (contentLength > 64 * 1024 * 1024) throw new Error("LAUNCH_PERSISTENT_SAVE_TOO_LARGE");
          persistentBytes = persistentResponse.status === 204 ? null : new Uint8Array(await persistentResponse.arrayBuffer());
          if (persistentBytes && persistentBytes.byteLength > 64 * 1024 * 1024) throw new Error("LAUNCH_PERSISTENT_SAVE_TOO_LARGE");
        }

        if (!stage.current || !frameRef.current) return;
        const frame = frameRef.current;
        const frameWindow = frame.contentWindow;
        const frameDocument = frame.contentDocument;
        if (!frameWindow || !frameDocument) throw new Error("PLAYER_FRAME_UNAVAILABLE");
        frameDocument.documentElement.lang = "zh-CN";
        frameDocument.documentElement.classList.add("retrom-native-menu-locked");
        const style = frameDocument.createElement("style");
        style.textContent = `
html,body,#game,#retrom-emulator,.ejs_parent,.ejs_game,.ejs_canvas_parent{width:100%!important;height:100%!important;margin:0!important;overflow:hidden;background:#05060a}
.ejs_canvas_parent{display:grid!important;place-items:center!important}
canvas{display:block;max-width:none!important;max-height:none!important;margin:auto!important}
html.retrom-native-menu-locked:not(.retrom-native-settings-open) .ejs_menu_bar{visibility:hidden!important;opacity:0!important;pointer-events:none!important}
html.retrom-native-menu-locked.retrom-native-settings-open .ejs_menu_bar{border:0!important;background:transparent!important;box-shadow:none!important;pointer-events:none!important}
html.retrom-native-menu-locked.retrom-native-settings-open .ejs_menu_bar>*{visibility:hidden!important;pointer-events:none!important}
html.retrom-native-menu-locked.retrom-native-settings-open .ejs_menu_bar>:has(>.ejs_settings_parent){visibility:visible!important}
html.retrom-native-menu-locked.retrom-native-settings-open .ejs_menu_bar>:has(>.ejs_settings_parent)>.ejs_menu_button{visibility:hidden!important;pointer-events:none!important}
html.retrom-native-menu-locked.retrom-native-settings-open .ejs_menu_bar .ejs_settings_parent{visibility:visible!important;pointer-events:auto!important}
`;
        frameDocument.head.append(style);
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
            emulator.current = instance;
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
                if (!fs) { setState("error"); setMessage("LAUNCH_PERSISTENT_SAVE_FS_UNAVAILABLE"); return; }
                mountedSaveFS = fs;
              });
              instance.on("saveSaveFiles", (value: unknown) => {
                if (value instanceof Uint8Array && value.byteLength) uploadPersistent(value, "AUTO_INTERVAL");
              });
            }
            instance.on("exit", () => { void sendEvent("finish"); });
          },
          onGameStart: () => {
            frameDocument.documentElement.classList.add("retrom-native-menu-locked");
            emulator.current?.menu?.close?.();
            const manager = emulator.current?.gameManager;
            if (config.persistentSaveMode !== "NONE") {
              const savePath = manager?.getSaveFilePath?.();
              if (!manager || !mountedSaveFS || !savePath) { setState("error"); setMessage("LAUNCH_PERSISTENT_SAVE_LOAD_FAILED"); return; }
              try {
                restorePersistentSave(manager, mountedSaveFS, savePath, persistentBytes);
              } catch {
                setState("error");
                setMessage("LAUNCH_PERSISTENT_SAVE_LOAD_FAILED");
                return;
              }
            }
            if (emulator.current) emulator.current.paused = false;
            pausedRef.current = false;
            setPaused(false);
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
          },
          onSaveState: (payload) => { void uploadManualState(payload); },
          onSaveSave: config.persistentSaveMode === "NONE" ? undefined : (payload) => { if (payload.save.byteLength) uploadPersistent(payload.save, "MANUAL_EXPORT"); }
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
      closeEmulatorSettingsPanels(emulator.current);
      nativeMenuObserver?.disconnect();
      if (heartbeat.current !== null) window.clearInterval(heartbeat.current);
      if (toastTimer.current !== null) window.clearTimeout(toastTimer.current);
    };
  }, [exit, launchId, revealControlsAtTopEdge, sendEvent, showControls, uploadManualState, uploadPersistent]);

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

  return (
    <main className={`player-shell${paused ? " is-paused" : ""}`} onKeyDown={showControls} onPointerMove={(event) => revealControlsAtTopEdge(event.clientY)}>
      <PlayerChrome
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
        onHoldControls={holdControls}
        onReleaseControls={releaseControls}
        onSave={saveManualState}
        onPauseForToolbarInteraction={pauseForToolbarInteraction}
        onToggleFullscreen={() => void toggleFullscreen()}
        onOpenEmulatorSettings={openEmulatorSettings}
        onCloseEmulatorSettings={closeEmulatorSettings}
        onOpenEmulatorPanel={openEmulatorPanel}
        onChangeEmulatorVolume={changeEmulatorVolume}
        onToggleEmulatorMute={toggleEmulatorMute}
        onExit={() => void exit()}
        onDownloadConflict={downloadConflictingSave}
      />
      <div className="player-stage" ref={stage} onClick={handleGameSurfaceInteraction}>
        <iframe ref={frameRef} title="Retrom EmulatorJS Player" className="player-frame" src="about:blank" />
        {state !== "running" ? <div className="player-loading">{state === "loading" ? <i /> : null}<strong>{message}</strong><p>{state === "error" ? <><span>凭据可能已过期或依赖不兼容。</span> <Link href="/library">返回游戏库</Link></> : "页面会在验证和存档预取后自动开始，无需再次点击。"}</p></div> : null}
      </div>
    </main>
  );
}
