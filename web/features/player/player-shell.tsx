"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type RefObject } from "react";
import { useAuth } from "@/features/auth/auth-provider";
import { markImmersivePlayerReturn } from "@/features/immersive/active-gamepad";
import { captureManualScreenshot, type DiscSet, type DiscState, type EmulatorInstance, type ManualScreenshot, type ManualStatePayload, type PlayerConfig } from "./adapters/ejs-4.2.3-v2";
import { reportMultiDiscPlayerEvent, type MultiDiscPlayerEvent } from "./multi-disc-telemetry";
import { setEmulatorPaused } from "./pause-control";
import { captureBeforePause } from "./pause-screenshot";
import { PlayerChrome, type PlayerChromeProps, type PlayerDebugRuntime } from "./player-chrome";
import { shouldRevealPlayerControls, shouldRevealPlayerControlsForKey } from "./player-controls-visibility";
import type { PlayerDebugMetrics } from "./player-debug";
import { applyVideoRenderingMode, readVideoRenderingMode, subscribeVideoRenderingMode, type VideoRenderingMode } from "./video-rendering";
import {
  initialPlayerOrientationState,
  type PlayerOrientationState,
} from "./orientation";
import type { NetplayController } from "./netplay/controller";
import { usePlayerBootstrap } from "./player-bootstrap";
import { usePlayerSession } from "./player-session";
import { usePlayerRuntimeActions } from "./player-runtime-actions";
import { usePlayerOrientationRuntime } from "./player-orientation-runtime";
import { usePlayerRuntimeEffects } from "./player-runtime-effects";
import { ImmersivePlayerMenu } from "./immersive-player-menu";
import { useImmersivePlayer } from "./use-immersive-player";
import { usePlayerKeyboardPause } from "./use-player-keyboard-pause";
import { saveImmersivePlayerState } from "./immersive-player-save";
import { RpgRuntimeValidationPanel } from "./rpg-runtime-validation-panel";
import type { RpgRuntimeValidationDriver } from "./rpg-runtime-validation";
export { readBoundedResponse, reportsNativeExit } from "./player-shell-model";

type ShellState = "loading" | "running" | "error";
type Mutable<T> = { current: T };
type UploadManualState = (payload: ManualStatePayload) => Promise<boolean>;

const initialDebugRuntime: PlayerDebugRuntime = {
  runtimeFamily: "",
  coreId: "", coreArtifactId: "", emulatorJSVersion: "", playerAdapterId: "", inputMode: "",
  crossOriginIsolated: false, sharedArrayBuffer: false,
};

function useImmersiveSaveActions(
  emulatorRef: Mutable<EmulatorInstance | undefined>,
  manualSaveAvailableRef: Mutable<boolean>,
  pauseCaptureRef: Mutable<Promise<ManualScreenshot | null>>,
  lastScreenshotRef: Mutable<ManualScreenshot | null>,
  uploadManualState: UploadManualState,
) {
  const beginImmersivePauseCapture = useCallback(() => {
    const current = emulatorRef.current;
    lastScreenshotRef.current = null;
    if (!current) {pauseCaptureRef.current = Promise.resolve(null); return;}
    const capture = captureManualScreenshot(current).then((screenshot) => {
      lastScreenshotRef.current = screenshot;
      return screenshot;
    });
    pauseCaptureRef.current = captureBeforePause(capture, () => undefined);
  }, [emulatorRef, lastScreenshotRef, pauseCaptureRef]);
  const saveImmersiveGame = useCallback(() => saveImmersivePlayerState(
    emulatorRef.current, manualSaveAvailableRef.current, uploadManualState, pauseCaptureRef.current,
  ), [emulatorRef, manualSaveAvailableRef, pauseCaptureRef, uploadManualState]);
  return { beginImmersivePauseCapture, saveImmersiveGame };
}

export function PlayerShell({ launchId, experience = "standard" }: { launchId: string; experience?: "standard" | "immersive" }) {
  const router = useRouter();
  const { context } = useAuth();
  const userId = context.user?.userId;
  const stage = useRef<HTMLDivElement>(null);
  const frameRef = useRef<HTMLIFrameElement>(null);
  const orientationButtonRef = useRef<HTMLButtonElement>(null);
  const emulator = useRef<EmulatorInstance | undefined>(undefined);
  const [state, setState] = useState<ShellState>("loading");
  const [immersiveReturnTo, setImmersiveReturnTo] = useState("/immersive");
  const [message, setMessage] = useState("正在验证运行快照…");
  const [warnings, setWarnings] = useState<string[]>([]);
  const [toast, setToast] = useState("");
  const [syncText, setSyncText] = useState("正在连接…");
  const [syncTone, setSyncTone] = useState<"synced" | "busy" | "warning">("busy");
  const [saveUploadProgress, setSaveUploadProgress] = useState<number | null>(null);
  const [manualSaveAvailable, setManualSaveAvailable] = useState(true);
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
  const [rpgValidationDriver, setRpgValidationDriver] = useState<RpgRuntimeValidationDriver | null>(null);
  const [orientationState, setOrientationState] = useState<PlayerOrientationState>(initialPlayerOrientationState);
  const [orientationHelp, setOrientationHelp] = useState("若浏览器不能自动锁定方向，请手动旋转设备。");
  const [debugMetrics, setDebugMetrics] = useState<PlayerDebugMetrics | null>(null);
  const [debugRuntime, setDebugRuntime] = useState<PlayerDebugRuntime>(initialDebugRuntime);
  const returnTo = useRef("/library");
  const playerMode = useRef<PlayerConfig["mode"]>("single");
  const netplayConfig = useRef<NonNullable<PlayerConfig["netplay"]> | null>(null);
  const netplayController = useRef<NetplayController | null>(null);
  const sequence = useRef(0);
  const started = useRef(false);
  const finishing = useRef(false);
  const heartbeat = useRef<number | null>(null);
  const saveUploadQueue = useRef(Promise.resolve());
  const manualSaveAvailableRef = useRef(true);
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
  const keyboardPauseAction = useRef<() => void>(() => undefined);

  const reportPlayerEvent = useCallback((event: MultiDiscPlayerEvent) => {
    void reportMultiDiscPlayerEvent(launchId, event).catch(() => undefined);
  }, [launchId]);

  const clearControlsTimer = useCallback(() => {
    if (controlsTimer.current !== null) {window.clearTimeout(controlsTimer.current);}
    controlsTimer.current = null;
  }, []);

  const showControls = useCallback(() => {
    setControlsVisible(true);
    clearControlsTimer();
    if (running.current && !pausedRef.current && !chromePinned.current) {
      controlsTimer.current = window.setTimeout(() => {
        if (!pausedRef.current && !chromePinned.current) {setControlsVisible(false);}
      }, 2_000);
    }
  }, [clearControlsTimer]);

  const revealControlsAtTopEdge = useCallback((clientY: number) => {
    if (shouldRevealPlayerControls(clientY)) {showControls();}
  }, [showControls]);

  const showToast = useCallback((value: string, timeout = 2_400) => {
    if (toastTimer.current !== null) {window.clearTimeout(toastTimer.current);}
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
    clearControlsTimer();
    setControlsVisible(!running.current || pausedRef.current);
  }, [clearControlsTimer]);

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
    if (playerMode.current === "netplay") {return;}
    if (!running.current || pausedRef.current || pausePending.current || !emulator.current) {return;}
    const current = emulator.current;
    pausePending.current = true;
    const capture = captureManualScreenshot(current).then((screenshot) => {
      lastManualScreenshot.current = screenshot;
      return screenshot;
    });
    pauseCapture.current = captureBeforePause(capture, () => {
      pausePending.current = false;
      if (!running.current || pausedRef.current || !setEmulatorPaused(current, true)) {return;}
      pausedRef.current = true;
      setPaused(true);
      showToast("游戏已暂停，点击游戏画面继续");
      setControlsVisible(true);
      clearControlsTimer();
    });
  }, [clearControlsTimer, showToast]);

  const handleGameSurfaceInteraction = useCallback(() => {
    if (playerMode.current === "netplay") {return;}
    if (!running.current) {return;}
    if (!pausedRef.current || !setEmulatorPaused(emulator.current, false)) {return;}
    pausedRef.current = false;
    lastManualScreenshot.current = null;
    setPaused(false);
    showToast("游戏已继续");
    showControls();
  }, [showControls, showToast]);

  const onKeyboardPause = useCallback(() => keyboardPauseAction.current(), []);
  const replaceImmersiveRoute = useCallback((url: string) => {
    markImmersivePlayerReturn();
    router.replace(url);
  }, [router]);

  const sessionParams = useMemo(() => ({
    launchId, emulator, playerMode, sequence, started, finishing, saveUploadQueue, discSetRef,
    orientationStateRef, returnTo, netplayController, setOrientationState, setSaveUploadProgress,
    setSyncText, setSyncTone, showToast, replaceImmersiveRoute,
  }), [launchId, replaceImmersiveRoute, showToast]);
  const { sendEvent, uploadManualState, uploadValidationCheckpoint, exit, exitStrict } = usePlayerSession(sessionParams);
  const { beginImmersivePauseCapture, saveImmersiveGame } = useImmersiveSaveActions(emulator, manualSaveAvailableRef, pauseCapture, lastManualScreenshot, uploadManualState);
  const handleImmersiveFatal = useImmersiveFatalHandler(setMessage, setState);
  const immersive = useImmersivePlayer({
    enabled: experience === "immersive",
    emulator,
    pausedRef,
    running: state === "running",
    setPaused,
    exitStrict,
    saveAvailable: manualSaveAvailable,
    saveGame: saveImmersiveGame,
    beforeMenuPause: beginImmersivePauseCapture,
    onFatalError: handleImmersiveFatal,
  });

  useEffect(() => {
    videoRenderingModeRef.current = videoRenderingMode;
    const canvas = emulator.current?.canvas ?? frameRef.current?.contentDocument?.querySelector<HTMLCanvasElement>("canvas") ?? null;
    applyVideoRenderingMode(emulator.current, canvas, videoRenderingMode);
  }, [videoRenderingMode]);

  const bootstrapParams = useMemo(() => ({
    launchId, experience, immersiveGamepadFilter: immersive.filter, stage, frameRef, emulator, returnTo, playerMode, manualSaveAvailableRef, netplayConfig, discSetRef,
    orientationStateRef, videoRenderingModeRef, lastAudibleVolume, pausedRef, started, finishing, heartbeat,
    toastTimer, netplayController, netplayPausedRef, setMessage, setState, setManualSaveAvailable,
    setNetplayPlayerNo, setWarnings, setGameTitle, setCoreName, setPlatformName, setDebugRuntime, setDiscSet,
    setDiscState, setOrientationState, setFrameEnabled, setSyncText, setSyncTone, setEmulatorVolume,
    setEmulatorMuted, setPaused, setNetplayPaused, setImmersiveReturnTo, reportPlayerEvent, revealControlsAtTopEdge, showControls,
    setRpgValidationDriver, sendEvent, uploadManualState, uploadValidationCheckpoint,
    onKeyboardPause, onImmersiveMenuShortcut: immersive.requestMenu,
  }), [experience, immersive.filter, immersive.requestMenu, launchId, onKeyboardPause, reportPlayerEvent, revealControlsAtTopEdge, sendEvent, showControls, uploadManualState, uploadValidationCheckpoint]);

  usePlayerBootstrap(bootstrapParams);

  const runtimeEffectParams = useMemo(() => ({
    state, debugOpen, orientationBlocked: orientationState.phase === "orientation-blocked", emulator, frameRef,
    orientationButtonRef, running, pausedRef, chromePinned, controlsTimer, playerMode, netplayController,
    clearControlsTimer, setControlsVisible, setFullscreen, setDebugOpen, setDebugMetrics,
  }), [clearControlsTimer, debugOpen, orientationState.phase, state]);
  const { toggleDebug } = usePlayerRuntimeEffects(runtimeEffectParams);

  const runtimeActionParams = useMemo(() => ({
    userId, state, emulator, frameRef, manualSaveAvailableRef, pauseCapture, lastManualScreenshot,
    uploadManualState, discSetRef, discState, setDiscState, reportPlayerEvent, showToast, setSyncText,
    setSyncTone, setEmulatorToolbarOpen, holdControls, releaseControls, lastAudibleVolume, emulatorVolume,
    emulatorMuted, setEmulatorVolume, setEmulatorMuted, videoRenderingModeRef, netplayConfig,
    netplayPaused, netplayPausedRef, setNetplayPaused,
  }), [discState, emulatorMuted, emulatorVolume, holdControls, netplayPaused, releaseControls, reportPlayerEvent, showToast, state, uploadManualState, userId]);
  const actions = usePlayerRuntimeActions(runtimeActionParams);
  const toggleNetplayPause = actions.toggleNetplayPause;

  usePlayerKeyboardPause({
    emulator, keyboardPauseActionRef: keyboardPauseAction, playerMode, running, chromePinned, pausePending,
    netplayPlayerNo, pausedRef, lastManualScreenshot, setPaused, setControlsVisible,
    clearControlsTimer, showControls, showToast, toggleNetplayPause,
  });

  const orientationParams = useMemo(() => ({
    frameRef, playerMode, netplayController, emulator, pausedRef, netplayPausedRef, orientationStateRef,
    setOrientationState, setPaused, setOrientationHelp, requestNetplayPause: actions.requestNetplayPause,
    showControls, showToast,
  }), [actions.requestNetplayPause, showControls, showToast]);
  const { retryLandscape } = usePlayerOrientationRuntime(orientationParams);

  const chromeProps: PlayerChromeProps = {
    controlsVisible, running: state === "running", paused, fullscreen, gameTitle, coreName, platformName,
    syncText, syncTone, saveUploadProgress, saveAvailable: manualSaveAvailable, toast, warnings,
    emulatorToolbarOpen, emulatorVolume, emulatorMuted, videoRenderingMode, discSet, discState,
    netplayPlayerNo, netplayPaused, debugOpen, debugMetrics, debugRuntime, runtimeState: state,
    onHoldControls: holdControls, onReleaseControls: releaseControls, onToggleControls: toggleControls,
    onSave: actions.saveManualState, onPauseForToolbarInteraction: pauseForToolbarInteraction,
    onToggleFullscreen: () => void actions.toggleFullscreen(), onOpenEmulatorSettings: actions.openEmulatorSettings,
    onCloseEmulatorSettings: actions.closeEmulatorSettings, onOpenEmulatorPanel: actions.openEmulatorPanel,
    onChangeEmulatorVolume: actions.changeEmulatorVolume, onToggleEmulatorMute: actions.toggleEmulatorMute,
    onChangeVideoRenderingMode: actions.changeVideoRenderingMode, onSelectDisc: actions.selectDisc,
    onToggleNetplayPause: () => void actions.toggleNetplayPause(), onToggleDebug: toggleDebug,
    onGameSurface: handleGameSurfaceInteraction, onExit: () => void exit(),
  };
  return <PlayerShellView experience={experience} immersive={immersive} paused={paused} orientationState={orientationState} chromeProps={chromeProps} stage={stage} frameRef={frameRef} frameEnabled={frameEnabled} state={state} message={message} returnTo={experience === "immersive" ? immersiveReturnTo : "/library"} gameTitle={gameTitle} orientationHelp={orientationHelp} orientationButtonRef={orientationButtonRef} rpgValidationDriver={rpgValidationDriver} onShowControls={showControls} onRevealControls={revealControlsAtTopEdge} onSurface={handleGameSurfaceInteraction} onRetryLandscape={() => void retryLandscape()} />;
}

function useImmersiveFatalHandler(setMessage: (message: string) => void, setState: (state: ShellState) => void) {
  return useCallback((error: string) => {
    setMessage(error);
    setState("error");
  }, [setMessage, setState]);
}

type ImmersiveController = ReturnType<typeof useImmersivePlayer>;

function PlayerShellView({ experience, immersive, paused, orientationState, chromeProps, stage, frameRef, frameEnabled, state, message, returnTo, gameTitle, orientationHelp, orientationButtonRef, rpgValidationDriver, onShowControls, onRevealControls, onSurface, onRetryLandscape }: { experience: "standard" | "immersive"; immersive: ImmersiveController; paused: boolean; orientationState: PlayerOrientationState; chromeProps: PlayerChromeProps; stage: RefObject<HTMLDivElement | null>; frameRef: RefObject<HTMLIFrameElement | null>; frameEnabled: boolean; state: ShellState; message: string; returnTo: string; gameTitle: string; orientationHelp: string; orientationButtonRef: RefObject<HTMLButtonElement | null>; rpgValidationDriver: RpgRuntimeValidationDriver | null; onShowControls: () => void; onRevealControls: (clientY: number) => void; onSurface: () => void; onRetryLandscape: () => void }) {
  const blocked = orientationState.phase === "orientation-blocked";
  const isImmersive = experience === "immersive";
  return <main className={`player-shell${isImmersive ? " is-immersive" : ""}${paused ? " is-paused" : ""}${blocked ? " is-orientation-blocked" : ""}`} onKeyDown={(event) => {if (!isImmersive && shouldRevealPlayerControlsForKey(event.key)) {onShowControls();}}} onPointerMove={(event) => {if (!isImmersive) {onRevealControls(event.clientY);}}}>
    {!blocked && !isImmersive ? <PlayerChrome {...chromeProps} /> : null}
    <PlayerStage blocked={blocked} stage={stage} frameRef={frameRef} frameEnabled={frameEnabled} state={state} message={message} returnTo={returnTo} immersive={isImmersive} onSurface={isImmersive ? () => undefined : onSurface} />
    {!blocked && rpgValidationDriver ? <RpgRuntimeValidationPanel driver={rpgValidationDriver} /> : null}
    {!blocked && isImmersive ? <ImmersivePlayerMenu overlay={immersive.overlay} saveAvailable={immersive.saveAvailable} onCancel={immersive.menuCancel} onSelect={immersive.menuSelect} onConfirm={immersive.runSelectedMenuAction} /> : null}
    {blocked ? <OrientationGate state={orientationState} gameTitle={gameTitle} help={orientationHelp} buttonRef={orientationButtonRef} onRetry={onRetryLandscape} /> : null}
  </main>;
}

function PlayerStage({ blocked, stage, frameRef, frameEnabled, state, message, returnTo, immersive, onSurface }: { blocked: boolean; stage: RefObject<HTMLDivElement | null>; frameRef: RefObject<HTMLIFrameElement | null>; frameEnabled: boolean; state: ShellState; message: string; returnTo: string; immersive: boolean; onSurface: () => void }) {
  return <div className="player-stage" ref={stage} inert={blocked ? true : undefined} aria-hidden={blocked || undefined} onClick={onSurface}>{frameEnabled ? <iframe ref={frameRef} title="Retrom 游戏 Player" className="player-frame" src="about:blank" /> : null}{state !== "running" ? <PlayerLoading state={state} message={message} returnTo={returnTo} immersive={immersive} /> : null}</div>;
}

function PlayerLoading({ state, message, returnTo, immersive }: { state: ShellState; message: string; returnTo: string; immersive: boolean }) {
  return <div className="player-loading">{state === "loading" ? <i /> : null}<strong>{message}</strong><p>{state === "error" ? <><span>凭据可能已过期或依赖不兼容。</span> <Link href={returnTo}>{immersive ? "返回游戏列表" : "返回游戏库"}</Link></> : "页面会在验证和指定存档恢复后自动开始，无需再次点击。"}</p></div>;
}

function OrientationGate({ state, gameTitle, help, buttonRef, onRetry }: { state: PlayerOrientationState; gameTitle: string; help: string; buttonRef: RefObject<HTMLButtonElement | null>; onRetry: () => void }) {
  const activeP2 = state.runtimeKind === "netplay-p2" && state.started;
  const status = activeP2 ? "联机仍在进行" : state.started ? "游戏已暂停" : "移动 Player 需要横屏";
  return <section className="player-orientation-gate" role="dialog" aria-modal="true" aria-labelledby="player-orientation-title" onKeyDown={(event) => {if (event.key === "Tab") {event.preventDefault(); buttonRef.current?.focus();}}}><div className="player-rotate-mark" aria-hidden="true"><span>↻</span></div><p>{status}</p><h1 id="player-orientation-title">请横向握持设备开始游戏</h1><strong>{gameTitle}</strong><small>{activeP2 ? "你是 P2，不能暂停全局联机；本地输入已清空。" : help}</small><button ref={buttonRef} className="button" type="button" onClick={onRetry}>尝试进入全屏并横屏</button></section>;
}
