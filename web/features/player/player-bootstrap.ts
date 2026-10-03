"use client";

import {runHostStartup} from "./runtime/startup-task";
import type {RuntimeStartupTaskV1} from "./runtime/contract";
import {noSaveStatusText, type CheckpointSemantics} from "./checkpoint-semantics";
import {readContentLoading, resolveContentLoading, type ContentLoadingCapability} from "./content-loading";

import {useCallback, useEffect, useRef, useState, type Dispatch, type RefObject, type SetStateAction} from "react";
import {getImmersiveAudioPreferences} from "@/features/immersive/immersive-audio-preferences";
import type {ImmersiveGamepadFilter} from "./immersive-gamepad-filter";
import type {MultiDiscPlayerEvent} from "./multi-disc-telemetry";
import {mobilePlayerQuery, portraitPlayerQuery, reducePlayerOrientation, waitForStableLandscape, type PlayerOrientationState} from "./orientation";
import {useSerializedPlayerBootstrap} from "./player-bootstrap-lifecycle";
import {productCheckpointPresentation} from "./player-checkpoint-availability";
import type {PlayProgressClock} from "./play-progress-clock";
import type {PlayerDebugRuntime} from "./player-chrome";
import type {PlayerLoadProgress} from "./player-loading";
import type {LaunchEnvelopeV1, PlayerRuntimeV1, RuntimeCheckpointAvailabilityV1, RuntimeDiscStateV1, RuntimeEventV1, RuntimeFinalSnapshotV1, RuntimeVideoModeV1} from "./runtime/contract";
import {readLaunchConfig} from "./launch-config";
import {mountProviderRuntime, type RuntimeController} from "./runtime/runtime-controller";
import {installRuntimeE2EDiagnostics} from "./runtime/e2e-diagnostics";
import {installRuntimeSurfaceControls} from "./runtime/surface-controls";

type ShellState = "loading" | "running" | "error";
type SyncTone = "synced" | "busy" | "warning";
type Mutable<T> = {current: T};

export type PlayerBootstrapParams = {
  reportStartupTask?: (task: RuntimeStartupTaskV1 | null) => void;
  userId?: string;
  launchId: string;
  experience: "standard" | "immersive";
  immersiveGamepadFilter?: ImmersiveGamepadFilter;
  stage: RefObject<HTMLDivElement | null>;
  runtime: Mutable<PlayerRuntimeV1 | null>;
  runtimeController: Mutable<RuntimeController | null>;
  envelope: Mutable<LaunchEnvelopeV1 | null>;
  returnTo: Mutable<string>;
  manualSaveAvailableRef: Mutable<boolean>;
  programSelectionRequiredRef: Mutable<boolean>;
  orientationStateRef: Mutable<PlayerOrientationState>;
  videoRenderingModeRef: Mutable<RuntimeVideoModeV1>;
  pausedRef: Mutable<boolean>;
  started: Mutable<boolean>;
  finishing: Mutable<boolean>;
  progressTimer: Mutable<number | null>;
  progressClock: Mutable<PlayProgressClock>;
  toastTimer: Mutable<number | null>;
  setMessage: Dispatch<SetStateAction<string>>;
  setLoadProgress: Dispatch<SetStateAction<PlayerLoadProgress | null>>;
  setContentLoadingCapability: Dispatch<SetStateAction<ContentLoadingCapability | undefined>>;
  setState: Dispatch<SetStateAction<ShellState>>;
  setManualSaveAvailable: Dispatch<SetStateAction<boolean>>;
  setProgramSelectionRequired: Dispatch<SetStateAction<boolean>>;
  setWarnings: Dispatch<SetStateAction<string[]>>;
  setGameTitle: Dispatch<SetStateAction<string>>;
  setCheckpointSemantics?: Dispatch<SetStateAction<CheckpointSemantics>>;
  setCoreName: Dispatch<SetStateAction<string>>;
  setPlatformName: Dispatch<SetStateAction<string>>;
  setDebugRuntime: Dispatch<SetStateAction<PlayerDebugRuntime>>;
  setDiscState: Dispatch<SetStateAction<RuntimeDiscStateV1 | null>>;
  setOrientationState: Dispatch<SetStateAction<PlayerOrientationState>>;
  setSyncText: Dispatch<SetStateAction<string>>;
  setSyncTone: Dispatch<SetStateAction<SyncTone>>;
  setEmulatorVolume: Dispatch<SetStateAction<number>>;
  setEmulatorMuted: Dispatch<SetStateAction<boolean>>;
  setPaused: Dispatch<SetStateAction<boolean>>;
  setReviewScreenshotAvailable: Dispatch<SetStateAction<boolean>>;
  setPlayerReturnTo: Dispatch<SetStateAction<string>>;
  reportPlayerEvent: (event: MultiDiscPlayerEvent) => void;
  onKeyboardPause: () => void;
  onImmersiveMenuShortcut: () => void;
  onRevealControls: (clientY: number) => void;
  onShowControls: () => void;
  onGameSurface: () => void;
  onGamepadCursorReady?: (runtime: PlayerRuntimeV1) => void;
  onExitRequested: (snapshot?: RuntimeFinalSnapshotV1) => void;
  reportProgress: () => Promise<void>;
};

type BootstrapResources = {
  controller?: RuntimeController;
  surfaceControlsCleanup?: () => void;
  inputSubscription?: () => void;
  e2eDiagnosticsCleanup?: () => void;
};

export function usePlayerBootstrap(params: PlayerBootstrapParams, cancellationRef: Mutable<(() => Promise<void>) | null>) {
  const [attempt, setAttempt] = useState(0);
  const retrying = useRef(false);
  const cancel = useSerializedPlayerBootstrap(`${params.launchId}:${params.experience}:${attempt}`, params,
    createBootstrapResources, bootstrapPlayer, cleanupBootstrap, handleBootstrapError,
  );
  useEffect(() => {
    cancellationRef.current = cancel;
    return () => {if (cancellationRef.current === cancel) {cancellationRef.current = null;}};
  }, [cancel, cancellationRef]);
  useEffect(() => {retrying.current = false;}, [attempt]);
  return useCallback(() => {
    if (retrying.current) {return;}
    retrying.current = true;
    params.setState("loading");
    setAttempt((current) => current + 1);
  }, [params]);
}

function createBootstrapResources(): BootstrapResources {return {};}

async function bootstrapPlayer(params: PlayerBootstrapParams, resources: BootstrapResources, abort: AbortController) {
  params.reportStartupTask?.(null);
  params.setLoadProgress(null);
  params.setContentLoadingCapability(undefined);
  params.setMessage("正在验证 Provider 启动信息…");
  params.setState("loading");
  const envelope = await runHostStartup("LAUNCH_CONFIG", params.reportStartupTask,
    () => readLaunchConfig(params.launchId, abort.signal), abort.signal);
  params.setMessage("正在启动游戏…");
  validateExperience(params.experience, envelope);
  applyEnvelope(params, envelope);
  await prepareOrientation(params, abort.signal);
  if (!params.stage.current) {throw new Error("PLAYER_RUNTIME_FRAME_INVALID");}

  const mounted = await mountProviderRuntime(envelope, params.stage.current, {
    host: {contentLoading: resolveContentLoading(envelope.runtime.capabilities.contentLoading, readContentLoading(params.userId), envelope.session.purpose)},
    signal: abort.signal,
    onExitRequested: params.onExitRequested,
    onFatalError: (code) => {
      params.setMessage(code);
      params.setState("error");
    },
    onRuntimeEvent: (event) => handleRuntimeEvent(event, params),
  });
  if (abort.signal.aborted) {await mounted.exit(); return;}
  resources.controller = mounted;
  params.runtimeController.current = mounted;
  params.runtime.current = mounted.runtime;
  resources.e2eDiagnosticsCleanup = installRuntimeE2EDiagnostics(mounted.runtime);
  resources.surfaceControlsCleanup = installRuntimeSurfaceControls(mounted.runtime, {
    experience: params.experience,
    onKeyboardPause: params.onKeyboardPause,
    onImmersiveMenuShortcut: params.onImmersiveMenuShortcut,
    onRevealControls: params.onRevealControls,
    onShowControls: params.onShowControls,
    onSurface: params.onGameSurface,
  });
  params.onGamepadCursorReady?.(mounted.runtime);
  await runHostStartup("PLAYER_SETUP", params.reportStartupTask, () => configureMountedRuntime(params, resources, mounted.runtime), abort.signal);
  await completeSingleStart(params);
}

function applyEnvelope(params: PlayerBootstrapParams, envelope: LaunchEnvelopeV1) {
  params.envelope.current = envelope;
  params.setContentLoadingCapability(envelope.session.purpose === "PRODUCT" ? envelope.runtime.capabilities.contentLoading : undefined);
  params.returnTo.current = envelope.session.returnTo;
  params.setPlayerReturnTo(envelope.session.returnTo);
  params.setReviewScreenshotAvailable(envelope.session.purpose === "REVIEW_PREVIEW" && envelope.runtime.capabilities.screenshot);
  params.setWarnings(envelope.session.warnings);
  params.setGameTitle(envelope.session.title);
  params.setCoreName(envelope.session.coreName);
  params.setCheckpointSemantics?.(envelope.runtime.checkpoint === null ? "NO_SAVE" : envelope.runtime.checkpoint.semantics ?? "INSTANT");
  if (envelope.runtime.checkpoint === null) {
    params.manualSaveAvailableRef.current = false;
    params.setManualSaveAvailable(false);
    params.setSyncText(noSaveStatusText);
  } else if (envelope.runtime.checkpoint.semantics === "GAME_SAVE") {
    params.manualSaveAvailableRef.current = false;
    params.setManualSaveAvailable(false);
    params.setSyncText("请在游戏内保存");
  }
  params.setPlatformName(envelope.session.platformName);
  params.setDebugRuntime({
    providerId: envelope.runtime.providerId,
    providerVersion: envelope.runtime.providerVersion,
    targetId: envelope.runtime.targetId,
    crossOriginIsolated: window.crossOriginIsolated,
    sharedArrayBuffer: typeof SharedArrayBuffer !== "undefined",
  });
  params.programSelectionRequiredRef.current = false;
  params.setProgramSelectionRequired(false);
  params.setDiscState(null);
}

async function configureMountedRuntime(
  params: PlayerBootstrapParams,
  resources: BootstrapResources,
  runtime: PlayerRuntimeV1,
) {
  const capabilities = runtime.getCapabilities();
  if (capabilities.volume) {
    const preferences = params.experience === "immersive" ? getImmersiveAudioPreferences() : null;
    const volume = preferences?.gameVolume ?? 0.5;
    const muted = preferences?.gameMuted === true || volume === 0;
    await runtime.setVolume(muted ? 0 : volume);
    params.setEmulatorVolume(volume);
    params.setEmulatorMuted(muted);
  }
  if (capabilities.videoModes.includes(params.videoRenderingModeRef.current)) {
    await runtime.setVideoMode(params.videoRenderingModeRef.current);
  }
  if (params.experience === "immersive") {
    const policy = params.immersiveGamepadFilter;
    if (!policy || !capabilities.inputFilter) {throw new Error("PLAYER_IMMERSIVE_GAMEPAD_FILTER_UNAVAILABLE");}
    await runtime.setInputFilter(policy.getPolicy());
    resources.inputSubscription = policy.subscribe((next) => {void runtime.setInputFilter(next);});
  }
  if (capabilities.discSwitch) {
    const disc = await runtime.getDiscState();
    params.setDiscState(disc);
    params.reportPlayerEvent({eventType: "START", resultCode: "OK", discCount: disc.count, observedDiscCount: disc.count});
  }
}

async function completeSingleStart(params: PlayerBootstrapParams) {
  params.pausedRef.current = false;
  params.setPaused(false);
  applyStartedOrientation(params);
  params.progressClock.current.start(performance.now(), document.visibilityState === "visible");
  params.started.current = true;
  params.setState("running");
  const availability = params.runtime.current?.getCheckpointAvailability() ?? {available: false, reason: "UNSUPPORTED"};
  updateCheckpointAvailability(params, availability);
  void params.reportProgress();
  params.progressTimer.current = window.setInterval(() => {void params.reportProgress();}, 30_000);
}

function handleRuntimeEvent(event: RuntimeEventV1, params: PlayerBootstrapParams) {
  if (event.type === "LOAD_TASK") {params.reportStartupTask?.(event.task); return;}
  if (event.type === "LOAD_PROGRESS") {
    params.setLoadProgress(event.totalBytes === null ? null : {
      loadedBytes: event.loadedBytes, totalBytes: event.totalBytes,
    });
    return;
  }
  if (event.type === "CHECKPOINT_AVAILABILITY_CHANGED") {
    updateCheckpointAvailability(params, event.availability);
    return;
  }
  if (event.type === "DISC_CHANGED") {params.setDiscState(event.state); return;}
  if (event.type === "STATE_CHANGED") {
    const paused = event.state === "PAUSED";
    params.progressClock.current.setPaused(performance.now(), paused);
    params.pausedRef.current = paused;
    params.setPaused(paused);
  }
}

function updateCheckpointAvailability(params: PlayerBootstrapParams, availability: RuntimeCheckpointAvailabilityV1) {
  const programSelectionRequired = !availability.available && availability.requiredAction === "SELECT_PROGRAM";
  params.programSelectionRequiredRef.current = programSelectionRequired;
  params.setProgramSelectionRequired(programSelectionRequired);
  const available = availability.available;
  if (params.envelope.current?.runtime.checkpoint === null) {return;}
  if (params.envelope.current?.runtime.checkpoint?.semantics === "GAME_SAVE") {return;}
  params.manualSaveAvailableRef.current = available;
  params.setManualSaveAvailable(available);
  const presentation = productCheckpointPresentation(available);
  params.setSyncText(presentation.text);
  params.setSyncTone(presentation.tone);
}

async function prepareOrientation(params: PlayerBootstrapParams, signal: AbortSignal) {
  if (typeof window.matchMedia !== "function") {return;}
  const mobile = window.matchMedia(mobilePlayerQuery);
  const portrait = window.matchMedia(portraitPlayerQuery);
  let transition = reducePlayerOrientation(params.orientationStateRef.current, {
    type: "config-ready", mobile: mobile.matches, portrait: portrait.matches,
  });
  params.orientationStateRef.current = transition.state;
  params.setOrientationState(transition.state);
  if (transition.state.phase !== "orientation-blocked") {return;}
  params.setMessage("请横向握持设备开始游戏");
  await waitForStableLandscape(portrait, signal, (nextPortrait) => {
    transition = reducePlayerOrientation(params.orientationStateRef.current, {
      type: "orientation-stable", portrait: nextPortrait, paused: false,
    });
    params.orientationStateRef.current = transition.state;
    params.setOrientationState(transition.state);
  });
}

function applyStartedOrientation(params: PlayerBootstrapParams) {
  const transition = reducePlayerOrientation(params.orientationStateRef.current, {
    type: "runtime-started", paused: false,
  });
  params.orientationStateRef.current = transition.state;
  params.setOrientationState(transition.state);
}

function validateExperience(experience: "standard" | "immersive", envelope: LaunchEnvelopeV1) {
  if (experience !== "immersive") {return;}
  if (envelope.session.mode !== "SINGLE" || !envelope.session.returnTo.startsWith("/immersive/")) {
    throw new Error("PLAYER_IMMERSIVE_SINGLE_ONLY");
  }
}

function handleBootstrapError(error: unknown, abort: AbortController, params: PlayerBootstrapParams) {
  if (abort.signal.aborted) {return;}
  const code = error instanceof Error ? error.message : "PLAYER_RUNTIME_FAILED";
  params.setMessage(code);
  params.setState("error");
}

async function cleanupBootstrap(params: PlayerBootstrapParams, resources: BootstrapResources, abort: AbortController) {
  abort.abort();
  resources.surfaceControlsCleanup?.();
  resources.inputSubscription?.();
  resources.e2eDiagnosticsCleanup?.();
  if (params.progressTimer.current !== null) {window.clearInterval(params.progressTimer.current); params.progressTimer.current = null;}
  params.progressClock.current.stop(performance.now());
  params.started.current = false;
  if (params.toastTimer.current !== null) {window.clearTimeout(params.toastTimer.current); params.toastTimer.current = null;}
  await resources.controller?.exit().catch(() => undefined);
  if (params.runtimeController.current === resources.controller) {params.runtimeController.current = null;}
  if (params.runtime.current === resources.controller?.runtime) {params.runtime.current = null;}
  params.envelope.current = null;
}
