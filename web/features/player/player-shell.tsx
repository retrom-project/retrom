"use client";

import {useRouter} from "next/navigation";
import {useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type RefObject} from "react";
import {useAuth} from "@/features/auth/auth-provider";
import {markImmersivePlayerReturn} from "@/features/immersive/active-gamepad";
import {reportMultiDiscPlayerEvent, type MultiDiscPlayerEvent} from "./multi-disc-telemetry";
import {PlayerChrome, type PlayerChromeProps, type PlayerDebugRuntime, type PlayerDiscSet} from "./player-chrome";
import {shouldRevealPlayerControls, shouldRevealPlayerControlsForKey} from "./player-controls-visibility";
import type {PlayerDebugMetrics} from "./player-debug";
import {applyVideoRenderingMode, readVideoRenderingMode, subscribeVideoRenderingMode, type VideoRenderingMode} from "./video-rendering";
import {initialPlayerOrientationState, type PlayerOrientationState} from "./orientation";
import type {NetplayController} from "./netplay/controller";
import {usePlayerBootstrap} from "./player-bootstrap";
import {usePlayerSession} from "./player-session";
import {usePlayerRuntimeActions} from "./player-runtime-actions";
import {usePlayerOrientationRuntime} from "./player-orientation-runtime";
import {usePlayerRuntimeEffects} from "./player-runtime-effects";
import {useRuntimeExitHandler} from "./player-runtime-exit";
import {ImmersivePlayerMenu} from "./immersive-player-menu";
import {useImmersivePlayer} from "./use-immersive-player";
import {usePlayerKeyboardPause} from "./use-player-keyboard-pause";
import {RpgRuntimeValidationPanel} from "./rpg-runtime-validation-panel";
import type {RpgRuntimeValidationDriver} from "./rpg-runtime-validation";
import {PlayerLoading, type PlayerLoadProgress} from "./player-loading";
import type {LaunchEnvelopeV1, PlayerRuntimeV1, RuntimeDiscStateV1} from "./runtime/contract";
import type {RuntimeController} from "./runtime/runtime-controller";
import {captureRuntimeSave} from "./runtime/runtime-actions";
export {readBoundedResponse, reportsNativeExit} from "./player-shell-model";

type ShellState = "loading" | "running" | "error";

const initialDebugRuntime: PlayerDebugRuntime = {
  providerId: "", providerVersion: "", targetId: "", targetContractSha256: "",
  crossOriginIsolated: false, sharedArrayBuffer: false,
};

function useImmersiveRouteReplacement() {
  const router = useRouter();
  return useCallback((url: string) => {
    markImmersivePlayerReturn();
    router.replace(url);
  }, [router]);
}

export function PlayerShell({launchId, experience = "standard"}: {launchId: string; experience?: "standard" | "immersive"}) {
  const replaceImmersiveRoute = useImmersiveRouteReplacement();
  const {context} = useAuth();
  const userId = context.user?.userId;
  const stage = useRef<HTMLDivElement>(null);
  const orientationButtonRef = useRef<HTMLButtonElement>(null);
  const runtime = useRef<PlayerRuntimeV1 | null>(null);
  const runtimeController = useRef<RuntimeController | null>(null);
  const envelope = useRef<LaunchEnvelopeV1 | null>(null);
  const [state, setState] = useState<ShellState>("loading");
  const [immersiveReturnTo, setImmersiveReturnTo] = useState("/immersive");
  const [message, setMessage] = useState("正在验证 Provider 启动契约…");
  const [loadProgress, setLoadProgress] = useState<PlayerLoadProgress | null>(null);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [toast, setToast] = useState("");
  const [syncText, setSyncText] = useState("正在连接…");
  const [syncTone, setSyncTone] = useState<"synced" | "busy" | "warning">("busy");
  const [saveUploadProgress, setSaveUploadProgress] = useState<number | null>(null);
  const [manualSaveAvailable, setManualSaveAvailable] = useState(true);
  const [dosProgramMenu, setDosProgramMenu] = useState(false);
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
  const [discState, setDiscState] = useState<RuntimeDiscStateV1 | null>(null);
  const [netplayPlayerNo, setNetplayPlayerNo] = useState<number | null>(null);
  const [netplayPaused, setNetplayPaused] = useState(false);
  const [debugOpen, setDebugOpen] = useState(false);
  const [rpgValidationDriver, setRpgValidationDriver] = useState<RpgRuntimeValidationDriver | null>(null);
  const [orientationState, setOrientationState] = useState<PlayerOrientationState>(initialPlayerOrientationState);
  const [orientationHelp, setOrientationHelp] = useState("若浏览器不能自动锁定方向，请手动旋转设备。");
  const [debugMetrics, setDebugMetrics] = useState<PlayerDebugMetrics | null>(null);
  const [debugRuntime, setDebugRuntime] = useState<PlayerDebugRuntime>(initialDebugRuntime);
  const returnTo = useRef("/library");
  const playerMode = useRef<"single" | "netplay">("single");
  const netplayController = useRef<NetplayController | null>(null);
  const sequence = useRef(0);
  const started = useRef(false);
  const finishing = useRef(false);
  const heartbeat = useRef<number | null>(null);
  const playEventQueue = useRef(Promise.resolve());
  const saveUploadQueue = useRef(Promise.resolve());
  const manualSaveAvailableRef = useRef(true);
  const dosProgramMenuRef = useRef(false);
  const controlsTimer = useRef<number | null>(null);
  const toastTimer = useRef<number | null>(null);
  const running = useRef(false);
  const pausedRef = useRef(false);
  const pausePending = useRef(false);
  const chromePinned = useRef(false);
  const lastAudibleVolume = useRef(0.5);
  const netplayPausedRef = useRef(false);
  const orientationStateRef = useRef<PlayerOrientationState>(initialPlayerOrientationState);
  const videoRenderingModeRef = useRef<VideoRenderingMode>("pixel");
  const keyboardPauseAction = useRef<() => void>(() => undefined);

  const discSet = useMemo<PlayerDiscSet | null>(() => discState ? {
    count: discState.count,
    entries: discState.labels.map((label, index) => ({index, label})),
  } : null, [discState]);

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
    toastTimer.current = window.setTimeout(() => {setToast(""); toastTimer.current = null;}, timeout);
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
    } else {showControls();}
  }, [clearControlsTimer, controlsVisible, showControls]);

  const pauseForToolbarInteraction = useCallback(() => {
    if (playerMode.current === "netplay" || !running.current || pausedRef.current || pausePending.current) {return;}
    const active = runtime.current;
    if (!active?.getCapabilities().pause) {return;}
    pausePending.current = true;
    void active.pause().then(() => {
      pausedRef.current = true;
      setPaused(true);
      showToast("游戏已暂停，点击游戏画面继续");
      setControlsVisible(true);
      clearControlsTimer();
    }).catch(() => showToast("无法暂停游戏", 3_000)).finally(() => {pausePending.current = false;});
  }, [clearControlsTimer, showToast]);

  const handleGameSurfaceInteraction = useCallback(() => {
    if (playerMode.current === "netplay" || !running.current || !pausedRef.current) {return;}
    const active = runtime.current;
    if (!active?.getCapabilities().pause) {return;}
    void active.resume().then(() => {
      pausedRef.current = false;
      setPaused(false);
      showToast("游戏已继续");
      showControls();
    }).catch(() => showToast("无法继续游戏", 3_000));
  }, [showControls, showToast]);

  const sessionParams = useMemo(() => ({
    launchId, runtime, playerMode, sequence, started, finishing, heartbeat, playEventQueue, saveUploadQueue,
    orientationStateRef, returnTo, netplayController, setOrientationState, setSaveUploadProgress,
    setSyncText, setSyncTone, showToast, replaceImmersiveRoute,
  }), [launchId, replaceImmersiveRoute, showToast]);
  const {sendEvent, uploadManualState, uploadValidationCheckpoint, exit, exitStrict, exitImmersiveAfterRuntimeExit} = usePlayerSession(sessionParams);

  const exitRuntime = useCallback(async () => {
    await runtimeController.current?.exit().catch(() => undefined);
    await exit();
  }, [exit]);
  const exitImmersiveRuntimeStrict = useCallback(async () => {
    await runtimeController.current?.exit();
    await exitStrict();
  }, [exitStrict]);
  const exitImmersiveAfterProviderExit = useCallback(async () => {
    await runtimeController.current?.exit().catch(() => undefined);
    await exitImmersiveAfterRuntimeExit();
  }, [exitImmersiveAfterRuntimeExit]);
  const handleRuntimeExitRequested = useRuntimeExitHandler(
    manualSaveAvailableRef, setManualSaveAvailable, setSyncText, setSyncTone,
    experience, exitRuntime, exitImmersiveAfterProviderExit,
  );
  const handleImmersiveFatal = useCallback((error: string) => {setMessage(error); setState("error");}, []);
  const saveImmersiveGame = useCallback(async () => {
    const active = runtime.current;
    if (!active || !manualSaveAvailableRef.current) {return false;}
    return uploadManualState(await captureRuntimeSave(active));
  }, [uploadManualState]);
  const immersive = useImmersivePlayer({
    enabled: experience === "immersive", runtime, pausedRef, running: state === "running", setPaused,
    exitStrict: exitImmersiveRuntimeStrict, saveAvailable: manualSaveAvailable,
    saveGame: saveImmersiveGame, beforeMenuPause: () => undefined, onFatalError: handleImmersiveFatal,
  });

  const bootstrapParams = useMemo(() => ({
    launchId, experience, immersiveGamepadFilter: immersive.filter, stage, runtime, runtimeController, envelope,
    returnTo, playerMode, manualSaveAvailableRef, dosProgramMenuRef, orientationStateRef, videoRenderingModeRef,
    pausedRef, started, finishing, heartbeat, toastTimer, netplayController, netplayPausedRef,
    setMessage, setLoadProgress, setState, setManualSaveAvailable, setDosProgramMenu, setNetplayPlayerNo,
    setWarnings, setGameTitle, setCoreName, setPlatformName, setDebugRuntime, setDiscState, setOrientationState,
    setSyncText, setSyncTone, setEmulatorVolume, setEmulatorMuted, setPaused, setNetplayPaused,
    setImmersiveReturnTo, setRpgValidationDriver, reportPlayerEvent, onExitRequested: handleRuntimeExitRequested,
    sendEvent, uploadValidationCheckpoint,
  }), [experience, handleRuntimeExitRequested, immersive.filter, launchId, reportPlayerEvent, sendEvent, uploadValidationCheckpoint]);
  usePlayerBootstrap(bootstrapParams);

  const runtimeEffectParams = useMemo(() => ({
    state, debugOpen, orientationBlocked: orientationState.phase === "orientation-blocked", runtime,
    orientationButtonRef, running, pausedRef, chromePinned, controlsTimer, playerMode, netplayController,
    clearControlsTimer, setControlsVisible, setFullscreen, setDebugOpen, setDebugMetrics,
  }), [clearControlsTimer, debugOpen, orientationState.phase, state]);
  const {toggleDebug} = usePlayerRuntimeEffects(runtimeEffectParams);

  const runtimeActionParams = useMemo(() => ({
    userId, state, runtime, envelope, manualSaveAvailableRef, dosProgramMenuRef, uploadManualState,
    discState, setDiscState, reportPlayerEvent, showToast, setSyncText, setSyncTone,
    setEmulatorToolbarOpen, holdControls, releaseControls, lastAudibleVolume, emulatorVolume,
    emulatorMuted, setEmulatorVolume, setEmulatorMuted, videoRenderingModeRef,
    netplayPaused, netplayPausedRef, setNetplayPaused,
  }), [discState, emulatorMuted, emulatorVolume, holdControls, netplayPaused, releaseControls, reportPlayerEvent, showToast, state, uploadManualState, userId]);
  const actions = usePlayerRuntimeActions(runtimeActionParams);

  usePlayerKeyboardPause({
    runtime, keyboardPauseActionRef: keyboardPauseAction, playerMode, running, chromePinned, pausePending,
    netplayPlayerNo, pausedRef, setPaused, setControlsVisible, clearControlsTimer, showControls,
    showToast, toggleNetplayPause: actions.toggleNetplayPause,
  });

  const orientationParams = useMemo(() => ({
    playerMode, netplayController, runtime, pausedRef, netplayPausedRef, orientationStateRef,
    setOrientationState, setPaused, setOrientationHelp, requestNetplayPause: actions.requestNetplayPause,
    showControls, showToast,
  }), [actions.requestNetplayPause, showControls, showToast]);
  const {retryLandscape} = usePlayerOrientationRuntime(orientationParams);

  useEffect(() => {
    videoRenderingModeRef.current = videoRenderingMode;
    applyVideoRenderingMode(runtime.current, videoRenderingMode);
  }, [videoRenderingMode]);

  const chromeProps: PlayerChromeProps = {
    controlsVisible, running: state === "running", paused, fullscreen, gameTitle, coreName, platformName,
    syncText, syncTone, saveUploadProgress, saveAvailable: manualSaveAvailable, dosProgramMenu, toast, warnings,
    emulatorToolbarOpen, emulatorVolume, emulatorMuted, videoRenderingMode, discSet, discState,
    netplayPlayerNo, netplayPaused, debugOpen, debugMetrics, debugRuntime, runtimeState: state,
    onHoldControls: holdControls, onReleaseControls: releaseControls, onToggleControls: toggleControls,
    onSave: actions.saveManualState, onPauseForToolbarInteraction: pauseForToolbarInteraction,
    onToggleFullscreen: () => void actions.toggleFullscreen(), onOpenEmulatorSettings: actions.openEmulatorSettings,
    onCloseEmulatorSettings: actions.closeEmulatorSettings, onOpenEmulatorPanel: actions.openEmulatorPanel,
    onChangeEmulatorVolume: actions.changeEmulatorVolume, onToggleEmulatorMute: actions.toggleEmulatorMute,
    onChangeVideoRenderingMode: actions.changeVideoRenderingMode, onSelectDisc: actions.selectDisc,
    onToggleNetplayPause: () => void actions.toggleNetplayPause(), onToggleDebug: toggleDebug,
    onGameSurface: handleGameSurfaceInteraction, onExit: () => void exitRuntime(),
  };
  return <PlayerShellView experience={experience} immersive={immersive} paused={paused} orientationState={orientationState}
    chromeProps={chromeProps} stage={stage} state={state} message={message} loadProgress={loadProgress}
    returnTo={experience === "immersive" ? immersiveReturnTo : "/library"} gameTitle={gameTitle}
    orientationHelp={orientationHelp} orientationButtonRef={orientationButtonRef}
    rpgValidationDriver={rpgValidationDriver} onShowControls={showControls}
    onRevealControls={revealControlsAtTopEdge} onSurface={handleGameSurfaceInteraction}
    onRetryLandscape={() => void retryLandscape()} />;
}

type ImmersiveController = ReturnType<typeof useImmersivePlayer>;

function PlayerShellView({experience, immersive, paused, orientationState, chromeProps, stage, state, message, loadProgress, returnTo, gameTitle, orientationHelp, orientationButtonRef, rpgValidationDriver, onShowControls, onRevealControls, onSurface, onRetryLandscape}: {
  experience: "standard" | "immersive"; immersive: ImmersiveController; paused: boolean;
  orientationState: PlayerOrientationState; chromeProps: PlayerChromeProps; stage: RefObject<HTMLDivElement | null>;
  state: ShellState; message: string; loadProgress: PlayerLoadProgress | null; returnTo: string; gameTitle: string;
  orientationHelp: string; orientationButtonRef: RefObject<HTMLButtonElement | null>;
  rpgValidationDriver: RpgRuntimeValidationDriver | null; onShowControls: () => void;
  onRevealControls: (clientY: number) => void; onSurface: () => void; onRetryLandscape: () => void;
}) {
  const blocked = orientationState.phase === "orientation-blocked";
  const isImmersive = experience === "immersive";
  return <main className={`player-shell${isImmersive ? " is-immersive" : ""}${paused ? " is-paused" : ""}${blocked ? " is-orientation-blocked" : ""}`}
    onKeyDown={(event) => {if (!isImmersive && shouldRevealPlayerControlsForKey(event.key)) {onShowControls();}}}
    onPointerMove={(event) => {if (!isImmersive) {onRevealControls(event.clientY);}}}>
    {!blocked && !isImmersive ? <PlayerChrome {...chromeProps} /> : null}
    <PlayerStage blocked={blocked} stage={stage} state={state} message={message} loadProgress={loadProgress}
      returnTo={returnTo} immersive={isImmersive} onSurface={isImmersive ? () => undefined : onSurface} />
    {!blocked && rpgValidationDriver ? <RpgRuntimeValidationPanel driver={rpgValidationDriver} /> : null}
    {!blocked && isImmersive ? <ImmersivePlayerMenu overlay={immersive.overlay} saveAvailable={immersive.saveAvailable}
      onCancel={immersive.menuCancel} onSelect={immersive.menuSelect} onConfirm={immersive.runSelectedMenuAction} /> : null}
    {blocked ? <OrientationGate state={orientationState} gameTitle={gameTitle} help={orientationHelp}
      buttonRef={orientationButtonRef} onRetry={onRetryLandscape} /> : null}
  </main>;
}

function PlayerStage({blocked, stage, state, message, loadProgress, returnTo, immersive, onSurface}: {
  blocked: boolean; stage: RefObject<HTMLDivElement | null>; state: ShellState; message: string;
  loadProgress: PlayerLoadProgress | null; returnTo: string; immersive: boolean; onSurface: () => void;
}) {
  return <div className="player-stage" ref={stage} inert={blocked ? true : undefined} aria-hidden={blocked || undefined} onClick={onSurface}>
    {state !== "running" ? <PlayerLoading state={state} message={message} progress={loadProgress} returnTo={returnTo} immersive={immersive} /> : null}
  </div>;
}

function OrientationGate({state, gameTitle, help, buttonRef, onRetry}: {
  state: PlayerOrientationState; gameTitle: string; help: string;
  buttonRef: RefObject<HTMLButtonElement | null>; onRetry: () => void;
}) {
  const activeP2 = state.runtimeKind === "netplay-p2" && state.started;
  const status = activeP2 ? "联机仍在进行" : state.started ? "游戏已暂停" : "移动 Player 需要横屏";
  return <section className="player-orientation-gate" role="dialog" aria-modal="true" aria-labelledby="player-orientation-title"
    onKeyDown={(event) => {if (event.key === "Tab") {event.preventDefault(); buttonRef.current?.focus();}}}>
    <div className="player-rotate-mark" aria-hidden="true"><span>↻</span></div><p>{status}</p>
    <h1 id="player-orientation-title">请横向握持设备开始游戏</h1><strong>{gameTitle}</strong>
    <small>{activeP2 ? "你是 P2，不能暂停全局联机；本地输入已清空。" : help}</small>
    <button ref={buttonRef} className="button" type="button" onClick={onRetry}>尝试进入全屏并横屏</button>
  </section>;
}
