"use client";

import type {Dispatch, RefObject, SetStateAction} from "react";
import {getImmersiveAudioPreferences} from "@/features/immersive/immersive-audio-preferences";
import {sha256} from "@/lib/crypto";
import type {ImmersiveGamepadFilter} from "./immersive-gamepad-filter";
import type {MultiDiscPlayerEvent} from "./multi-disc-telemetry";
import {NetplayController} from "./netplay/controller";
import {parseNetplayProfile, type NetplayProfile} from "./netplay/controller-model";
import {mobilePlayerQuery, portraitPlayerQuery, reducePlayerOrientation, waitForStableLandscape, type PlayerOrientationState, type PlayerRuntimeKind} from "./orientation";
import {useSerializedPlayerBootstrap} from "./player-bootstrap-lifecycle";
import {productCheckpointPresentation} from "./player-checkpoint-availability";
import type {PlayerDebugRuntime} from "./player-chrome";
import type {PlayerLoadProgress} from "./player-loading";
import {RpgRuntimeValidationDriver} from "./rpg-runtime-validation";
import type {ValidationCheckpointReceipt} from "./rpg-validation-checkpoint-response";
import type {LaunchEnvelopeV1, PlayerRuntimeV1, RuntimeDiscStateV1, RuntimeEventV1, RuntimeVideoModeV1} from "./runtime/contract";
import {parseLaunchEnvelopeJSON} from "./runtime/envelope";
import {RuntimeNetplayPortAdapter} from "./runtime/netplay-port-adapter";
import {mountProviderRuntime, type RuntimeController} from "./runtime/runtime-controller";
import type {RuntimeSavePayload} from "./runtime/runtime-actions";
import {installRuntimeE2EDiagnostics} from "./runtime/e2e-diagnostics";
import {installRuntimeSurfaceControls} from "./runtime/surface-controls";

type ShellState = "loading" | "running" | "error";
type SyncTone = "synced" | "busy" | "warning";
type Mutable<T> = {current: T};

export type PlayerBootstrapParams = {
  launchId: string;
  experience: "standard" | "immersive";
  immersiveGamepadFilter?: ImmersiveGamepadFilter;
  stage: RefObject<HTMLDivElement | null>;
  runtime: Mutable<PlayerRuntimeV1 | null>;
  runtimeController: Mutable<RuntimeController | null>;
  envelope: Mutable<LaunchEnvelopeV1 | null>;
  returnTo: Mutable<string>;
  playerMode: Mutable<"single" | "netplay">;
  manualSaveAvailableRef: Mutable<boolean>;
  dosProgramMenuRef: Mutable<boolean>;
  orientationStateRef: Mutable<PlayerOrientationState>;
  videoRenderingModeRef: Mutable<RuntimeVideoModeV1>;
  pausedRef: Mutable<boolean>;
  started: Mutable<boolean>;
  finishing: Mutable<boolean>;
  heartbeat: Mutable<number | null>;
  toastTimer: Mutable<number | null>;
  netplayController: Mutable<NetplayController | null>;
  netplayPausedRef: Mutable<boolean>;
  setMessage: Dispatch<SetStateAction<string>>;
  setLoadProgress: Dispatch<SetStateAction<PlayerLoadProgress | null>>;
  setState: Dispatch<SetStateAction<ShellState>>;
  setManualSaveAvailable: Dispatch<SetStateAction<boolean>>;
  setDosProgramMenu: Dispatch<SetStateAction<boolean>>;
  setNetplayPlayerNo: Dispatch<SetStateAction<number | null>>;
  setWarnings: Dispatch<SetStateAction<string[]>>;
  setGameTitle: Dispatch<SetStateAction<string>>;
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
  setNetplayPaused: Dispatch<SetStateAction<boolean>>;
  setImmersiveReturnTo: Dispatch<SetStateAction<string>>;
  setRpgValidationDriver: Dispatch<SetStateAction<RpgRuntimeValidationDriver | null>>;
  reportPlayerEvent: (event: MultiDiscPlayerEvent) => void;
  onKeyboardPause: () => void;
  onImmersiveMenuShortcut: () => void;
  onRevealControls: (clientY: number) => void;
  onShowControls: () => void;
  onGameSurface: () => void;
  onExitRequested: () => void;
  sendEvent: (kind: "start" | "heartbeat" | "finish") => Promise<void>;
  uploadValidationCheckpoint: (payload: RuntimeSavePayload) => Promise<ValidationCheckpointReceipt>;
};

type BootstrapResources = {
  controller?: RuntimeController;
  surfaceControlsCleanup?: () => void;
  inputSubscription?: () => void;
  validationDriver?: RpgRuntimeValidationDriver;
  netplayController?: NetplayController;
  e2eDiagnosticsCleanup?: () => void;
};

export function usePlayerBootstrap(params: PlayerBootstrapParams) {
  useSerializedPlayerBootstrap(`${params.launchId}:${params.experience}`, params,
    createBootstrapResources, bootstrapPlayer, cleanupBootstrap, handleBootstrapError,
  );
}

function createBootstrapResources(): BootstrapResources {return {};}

async function bootstrapPlayer(params: PlayerBootstrapParams, resources: BootstrapResources, abort: AbortController) {
  params.setLoadProgress(null);
  params.setMessage("正在验证 Provider 启动契约…");
  const response = await fetch(`/runtime/launches/${params.launchId}/config`, {
    credentials: "same-origin", cache: "no-store", signal: abort.signal,
  });
  if (!response.ok) {throw new Error(`LAUNCH_CONFIG_${response.status}`);}
  const envelope = parseLaunchEnvelopeJSON(await response.text());
  validateExperience(params.experience, envelope);
  applyEnvelope(params, envelope);
  await prepareOrientation(params, envelope, abort.signal);
  if (!params.stage.current) {throw new Error("PLAYER_RUNTIME_FRAME_INVALID");}

  const validation = createValidationDriver(params, envelope, abort.signal);
  if (validation) {
    resources.validationDriver = validation;
    params.setRpgValidationDriver(validation);
    await validation.prepare();
  }
  const mounted = await mountProviderRuntime(envelope, params.stage.current, {
    signal: abort.signal,
    onExitRequested: params.onExitRequested,
    onFatalError: (code) => {
      void validation?.reportRuntimeFailure(new Error(code));
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
  await configureMountedRuntime(params, resources, envelope, mounted.runtime);
  if (validation) {
    void validation.attachRuntime(mounted.runtime).catch((error: unknown) => {
      params.setMessage(error instanceof Error ? error.message : "RPG_RUNTIME_VALIDATION_FAILED");
      params.setState("error");
    });
  }
}

function applyEnvelope(params: PlayerBootstrapParams, envelope: LaunchEnvelopeV1) {
  params.envelope.current = envelope;
  params.returnTo.current = envelope.session.returnTo;
  if (params.experience === "immersive") {params.setImmersiveReturnTo(envelope.session.returnTo);}
  params.playerMode.current = envelope.session.mode === "NETPLAY" ? "netplay" : "single";
  params.setNetplayPlayerNo(envelope.netplay?.playerNo ?? null);
  params.setWarnings(envelope.session.warnings);
  params.setGameTitle(envelope.session.title);
  params.setCoreName(envelope.session.coreName);
  params.setPlatformName(envelope.session.platformName);
  params.setDebugRuntime({
    providerId: envelope.runtime.providerId,
    providerVersion: envelope.runtime.providerVersion,
    targetId: envelope.runtime.targetId,
    targetContractSha256: envelope.runtime.targetContractSha256,
    crossOriginIsolated: window.crossOriginIsolated,
    sharedArrayBuffer: typeof SharedArrayBuffer !== "undefined",
  });
  const dosProgramMenu = envelope.runtime.targetId === "dosbox-pure" &&
    envelope.targetOptions.dosEntryPath === null;
  params.dosProgramMenuRef.current = dosProgramMenu;
  params.setDosProgramMenu(dosProgramMenu);
  params.setDiscState(null);
}

async function configureMountedRuntime(
  params: PlayerBootstrapParams,
  resources: BootstrapResources,
  envelope: LaunchEnvelopeV1,
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
  if (envelope.session.mode === "NETPLAY") {
    await startNetplay(params, resources, envelope, runtime);
    return;
  }
  await completeSingleStart(params, envelope.session.purpose === "RUNTIME_VALIDATION");
}

async function completeSingleStart(params: PlayerBootstrapParams, validating: boolean) {
  params.pausedRef.current = false;
  params.setPaused(false);
  applyStartedOrientation(params);
  await params.sendEvent("start");
  params.setState("running");
  const availability = params.runtime.current?.getCheckpointAvailability() ?? {available: false, reason: "UNSUPPORTED"};
  const canSave = !validating && availability.available;
  updateCheckpointAvailability(params, canSave);
  params.setSyncText(validating ? "运行验证进行中" : canSave ? "可创建存档" : "当前场景暂不可存档");
  params.setSyncTone(validating ? "busy" : canSave ? "synced" : "warning");
  params.heartbeat.current = window.setInterval(() => {void params.sendEvent("heartbeat");}, 30_000);
}

async function startNetplay(
  params: PlayerBootstrapParams,
  resources: BootstrapResources,
  envelope: LaunchEnvelopeV1,
  runtime: PlayerRuntimeV1,
) {
  const netplay = envelope.netplay;
  if (!netplay) {throw new Error("PLAYER_NETPLAY_CONFIG_INVALID");}
  const profile = parseNetplayProfile(netplay.profile);
  const profileDigest = await digestProfile(profile);
  const port = new RuntimeNetplayPortAdapter(await runtime.getNetplayPort());
  const config = {
    roomId: netplay.roomId, sessionId: netplay.sessionId, playerNo: netplay.playerNo,
    runtimeSocketUrl: netplay.socketUrl, netplayProfile: profile,
  };
  const holder: {current?: NetplayController} = {};
  const current = () => params.netplayController.current === holder.current;
  const controller = new NetplayController(config, profileDigest, port, {
    onStatus: (text, tone) => {if (current()) {params.setSyncText(text); params.setSyncTone(tone);}},
    onRunning: () => {
      if (!current()) {return;}
      params.netplayPausedRef.current = false;
      params.setNetplayPaused(false);
      applyStartedOrientation(params);
      if (params.started.current) {return;}
      void params.sendEvent("start").then(() => {
        params.setState("running");
        params.heartbeat.current = window.setInterval(() => {void params.sendEvent("heartbeat");}, 30_000);
      }).catch(() => {params.setState("error"); params.setMessage("PLAY_SESSION_EVENT_FAILED");});
    },
    onPaused: () => {if (current()) {params.netplayPausedRef.current = true; params.setNetplayPaused(true);}},
    onEnded: (reason) => {
      if (!current()) {return;}
      params.setSyncText("联机已结束"); params.setSyncTone("warning"); params.setMessage(reason);
      void params.sendEvent("finish").catch(() => undefined)
        .finally(() => window.setTimeout(() => window.location.replace(params.returnTo.current), 600));
    },
  });
  holder.current = controller;
  resources.netplayController = controller;
  params.netplayController.current = controller;
  params.setMessage("正在建立联机同步屏障…");
  await controller.start();
}

function handleRuntimeEvent(event: RuntimeEventV1, params: PlayerBootstrapParams) {
  if (event.type === "LOAD_PROGRESS") {
    params.setLoadProgress(event.totalBytes === null ? null : {
      loadedBytes: event.loadedBytes, totalBytes: event.totalBytes,
    });
    return;
  }
  if (event.type === "CHECKPOINT_AVAILABILITY_CHANGED" &&
    params.envelope.current?.session.purpose !== "RUNTIME_VALIDATION") {
    updateCheckpointAvailability(params, event.availability.available);
    return;
  }
  if (event.type === "DISC_CHANGED") {params.setDiscState(event.state); return;}
  if (event.type === "STATE_CHANGED") {
    const paused = event.state === "PAUSED";
    params.pausedRef.current = paused;
    params.setPaused(paused);
  }
}

function updateCheckpointAvailability(params: PlayerBootstrapParams, available: boolean) {
  params.manualSaveAvailableRef.current = available;
  params.setManualSaveAvailable(available);
  if (params.envelope.current?.session.purpose === "PRODUCT") {
    const presentation = productCheckpointPresentation(available);
    params.setSyncText(presentation.text);
    params.setSyncTone(presentation.tone);
  }
}

function createValidationDriver(params: PlayerBootstrapParams, envelope: LaunchEnvelopeV1, signal: AbortSignal) {
  if (envelope.session.purpose !== "RUNTIME_VALIDATION") {return null;}
  return new RpgRuntimeValidationDriver({
    envelope, signal, uploadCheckpoint: params.uploadValidationCheckpoint,
    finishOriginalLaunch: async () => {
      if (params.heartbeat.current !== null) {
        window.clearInterval(params.heartbeat.current);
        params.heartbeat.current = null;
      }
      await params.sendEvent("finish");
    },
  });
}

async function prepareOrientation(params: PlayerBootstrapParams, envelope: LaunchEnvelopeV1, signal: AbortSignal) {
  if (typeof window.matchMedia !== "function") {return;}
  const mobile = window.matchMedia(mobilePlayerQuery);
  const portrait = window.matchMedia(portraitPlayerQuery);
  const kind: PlayerRuntimeKind = envelope.session.mode === "SINGLE" ? "single" :
    envelope.netplay?.playerNo === 1 ? "netplay-p1" : "netplay-p2";
  let transition = reducePlayerOrientation(params.orientationStateRef.current, {
    type: "config-ready", mobile: mobile.matches, portrait: portrait.matches, runtimeKind: kind,
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

async function digestProfile(profile: NetplayProfile) {
  const bytes = new TextEncoder().encode(JSON.stringify(profile));
  const digest = await sha256(bytes);
  return [...digest].map((value) => value.toString(16).padStart(2, "0")).join("");
}

function handleBootstrapError(error: unknown, abort: AbortController, params: PlayerBootstrapParams) {
  if (abort.signal.aborted) {return;}
  const code = error instanceof Error ? error.message : "PLAYER_RUNTIME_FAILED";
  params.setMessage(code === "LAUNCH_CONFIG_401" ? "启动会话不可用，请从游戏详情或存档重新开始。" : code);
  params.setState("error");
}

async function cleanupBootstrap(params: PlayerBootstrapParams, resources: BootstrapResources, abort: AbortController) {
  abort.abort();
  resources.surfaceControlsCleanup?.();
  resources.inputSubscription?.();
  resources.e2eDiagnosticsCleanup?.();
  resources.netplayController?.dispose();
  if (params.netplayController.current === resources.netplayController) {params.netplayController.current = null;}
  if (params.heartbeat.current !== null) {window.clearInterval(params.heartbeat.current); params.heartbeat.current = null;}
  if (params.toastTimer.current !== null) {window.clearTimeout(params.toastTimer.current); params.toastTimer.current = null;}
  if (resources.validationDriver) {
    params.setRpgValidationDriver((current) => current === resources.validationDriver ? null : current);
  }
  await resources.controller?.exit().catch(() => undefined);
  if (params.runtimeController.current === resources.controller) {params.runtimeController.current = null;}
  if (params.runtime.current === resources.controller?.runtime) {params.runtime.current = null;}
  params.envelope.current = null;
}
