"use client";

import { useCallback, useEffect, type Dispatch, type SetStateAction } from "react";
import { newUuid } from "@/lib/crypto";
import type {PlayerRuntimeV1} from "./runtime/contract";
import type {RuntimeSavePayload} from "./runtime/runtime-actions";
import { uploadWithProgress, type SaveUploadProgress } from "./upload-with-progress";
import { maximumManualSaveScreenshotBytes, prepareManualSaveScreenshot } from "./manual-save-screenshot";
import { reducePlayerOrientation, unlockLandscape, type PlayerOrientationState } from "./orientation";
import type { NetplayController } from "./netplay/controller";
import { parseValidationCheckpointReceipt, type ValidationCheckpointReceipt } from "./rpg-validation-checkpoint-response";

const SAVE_UPLOAD_PRESENTATION_MS = 400;
const SAVE_UPLOAD_TIMEOUT_MS = 300_000;
type Mutable<T> = { current: T };
type SyncTone = "synced" | "busy" | "warning";

export type PlayerSessionParams = {
  launchId: string; runtime: Mutable<PlayerRuntimeV1 | null>; playerMode: Mutable<"single" | "netplay">;
  sequence: Mutable<number>; started: Mutable<boolean>; finishing: Mutable<boolean>;
  heartbeat: Mutable<number | null>; playEventQueue: Mutable<Promise<void>>; saveUploadQueue: Mutable<Promise<void>>;
  orientationStateRef: Mutable<PlayerOrientationState>; returnTo: Mutable<string>;
  netplayController: Mutable<NetplayController | null>;
  replaceImmersiveRoute: (url: string) => void;
  setOrientationState: Dispatch<SetStateAction<PlayerOrientationState>>; setSaveUploadProgress: Dispatch<SetStateAction<number | null>>;
  setSyncText: Dispatch<SetStateAction<string>>; setSyncTone: Dispatch<SetStateAction<SyncTone>>;
  showToast: (message: string, timeout?: number) => void;
};

export function usePlayerSession(params: PlayerSessionParams) {
  const sendEvent = useCallback((kind: "start" | "heartbeat" | "finish") => queuePlayerEvent(kind, params), [params]);

  const reportProgress = useCallback((progress: SaveUploadProgress) => {
    params.setSaveUploadProgress(progress.percent);
    params.setSyncText(`正在上传存档 ${progress.percent}%`);
    params.setSyncTone("busy");
  }, [params]);

  const uploadManualState = useCallback(async (payload: RuntimeSavePayload) => Boolean(await queueStateUpload(payload, false, params, reportProgress)), [params, reportProgress]);
  const uploadValidationCheckpoint = useCallback(async (payload: RuntimeSavePayload) => {
    const result = await queueStateUpload(payload, true, params, reportProgress);
    if (!result || result === true) {throw new Error("RPG_CHECKPOINT_UPLOAD_FAILED");}
    return result;
  }, [params, reportProgress]);

  const exit = useCallback(() => exitPlayer(params, sendEvent), [params, sendEvent]);
  const exitStrict = useCallback(() => exitImmersivePlayer(params, sendEvent), [params, sendEvent]);
  const exitImmersiveAfterRuntimeExit = useCallback(
    () => exitImmersivePlayer(params, sendEvent, false),
    [params, sendEvent],
  );

  usePageHideFinish(params);
  usePageExitProtection(params);
  return { sendEvent, uploadManualState, uploadValidationCheckpoint, exit, exitStrict, exitImmersiveAfterRuntimeExit };
}

function queuePlayerEvent(kind: "start" | "heartbeat" | "finish", params: PlayerSessionParams) {
  if (kind === "heartbeat" && params.finishing.current) {return Promise.resolve();}
  if (kind === "finish") {beginPlayerFinish(params);}
  const result = params.playEventQueue.current.then(() => {
    if (kind === "heartbeat" && params.finishing.current) {return;}
    return sendPlayerEvent(kind, params);
  });
  params.playEventQueue.current = result.catch(() => undefined);
  return result;
}

async function sendPlayerEvent(kind: "start" | "heartbeat" | "finish", params: PlayerSessionParams) {
  if (kind === "heartbeat" && !params.started.current) {throw new Error("PLAY_SESSION_NOT_STARTED");}
  const firstOrUnstartedFinish = kind === "start" || kind === "finish" && !params.started.current;
  const next = firstOrUnstartedFinish ? 0 : params.sequence.current + 1;
  const response = await fetch(`/runtime/launches/${params.launchId}/${kind}`, { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ clientSequence: next, clientObservedAtMs: Date.now(), previousInterval: firstOrUnstartedFinish ? null : { running: true, visible: document.visibilityState === "visible", paused: params.runtime.current?.getState() === "PAUSED" } }) });
  if (!response.ok) {throw new Error("PLAY_SESSION_EVENT_FAILED");}
  params.sequence.current = next;
  if (kind === "start") {params.started.current = true;}
  if (kind === "finish") {params.finishing.current = true;}
}

function beginPlayerFinish(params: PlayerSessionParams) {
  params.finishing.current = true;
  if (params.heartbeat.current !== null) {
    window.clearInterval(params.heartbeat.current);
    params.heartbeat.current = null;
  }
}

function queueStateUpload(payload: RuntimeSavePayload, validation: boolean, params: PlayerSessionParams, reportProgress: (progress: SaveUploadProgress) => void) {
  const result = params.saveUploadQueue.current.then(() => uploadState(payload, validation, params, reportProgress));
  params.saveUploadQueue.current = result.then(() => undefined, () => undefined);
  return result.catch(() => {
    params.setSaveUploadProgress(null); params.setSyncText("保存失败"); params.setSyncTone("warning");
    params.showToast("手动存档上传失败，服务器未创建不完整记录", 4_000);
    return false;
  });
}

async function exitPlayer(params: PlayerSessionParams, sendEvent: (kind: "start" | "heartbeat" | "finish") => Promise<void>) {
  if (params.finishing.current) {return;}
  beginPlayerFinish(params);
  const exiting = reducePlayerOrientation(params.orientationStateRef.current, { type: "exit" });
  params.orientationStateRef.current = exiting.state;
  params.setOrientationState(exiting.state);
  if (exiting.effects.includes("unlock")) {unlockLandscape();}
  try {
    if (params.playerMode.current === "netplay") {params.netplayController.current?.end();}
    await params.saveUploadQueue.current;
    await sendEvent("finish");
  } catch { /* expiry is already a terminal server state */ }
  if (document.fullscreenElement) {await document.exitFullscreen().catch(() => undefined);}
  window.location.replace(params.returnTo.current);
}

async function exitImmersivePlayer(
  params: PlayerSessionParams,
  sendEvent: (kind: "start" | "heartbeat" | "finish") => Promise<void>,
  strict = true,
) {
  if (params.finishing.current) {return;}
  const exiting = reducePlayerOrientation(params.orientationStateRef.current, { type: "exit" });
  params.orientationStateRef.current = exiting.state;
  params.setOrientationState(exiting.state);
  if (exiting.effects.includes("unlock")) {unlockLandscape();}
  try {
    await sendEvent("finish");
  } catch (error) {
    if (strict) {throw error;}
  }
  params.replaceImmersiveRoute(params.returnTo.current);
}

async function uploadState(payload: RuntimeSavePayload, validation: boolean, params: PlayerSessionParams, reportProgress: (progress: SaveUploadProgress) => void) {
  if (!payload.checkpoint.bytes.byteLength) {return rejectSave(params, "状态为空，未创建存档。");}
  const discIndex = await currentDiscIndex(params);
  if (discIndex === "unavailable") {return rejectSave(params, "无法读取当前光盘，未创建存档。");}
  const preparedScreenshot = await prepareManualSaveScreenshot({
    screenshot: payload.screenshot,
    format: screenshotExtension(payload.screenshot),
  });
  const uploadPayload = {
    screenshot: preparedScreenshot?.screenshot ?? new Blob(),
    format: preparedScreenshot?.format ?? "png",
  };
  const form = createSaveForm(payload, uploadPayload, discIndex, validation);
  const startedAt = performance.now();
  params.setSaveUploadProgress(0);
  await waitForSaveUploadPresentationTurn();
  let response: Awaited<ReturnType<typeof uploadWithProgress>>;
  try {
    response = await uploadWithProgress({
      url: `/runtime/launches/${params.launchId}/save-states`, method: "POST",
      headers: { "Idempotency-Key": newUuid() }, body: form,
      totalBytes: payload.checkpoint.bytes.byteLength + uploadPayload.screenshot.size,
      timeoutMs: SAVE_UPLOAD_TIMEOUT_MS, onProgress: reportProgress,
    });
  } finally {
    await waitForSaveUploadPresentation(startedAt);
    params.setSaveUploadProgress(null);
  }
  if (!response.ok) {return rejectSave(params, "手动存档失败，服务器未创建不完整记录");}
  params.setSyncText("已同步"); params.setSyncTone("synced");
  params.showToast(uploadPayload.screenshot.size ? "手动存档和截图已保存" : "手动存档已保存，未附带截图");
  return uploadResult(validation, response.body);
}

function uploadResult(validation: boolean, responseBody: string): true | ValidationCheckpointReceipt {
  return validation ? parseValidationCheckpointReceipt(responseBody) : true;
}

async function currentDiscIndex(params: PlayerSessionParams): Promise<number | undefined | "unavailable"> {
  const runtime = params.runtime.current;
  if (!runtime?.getCapabilities().discSwitch) {return undefined;}
  try {
    return (await runtime.getDiscState()).currentIndex;
  } catch {return "unavailable";}
}

export function createSaveForm(
  payload: RuntimeSavePayload,
  screenshot: {screenshot: Blob; format: string},
  discIndex: number | undefined,
  validation = false,
) {
  const form = new FormData();
  const metadata = {
    checkpointFormat: payload.checkpoint.format,
    ...(validation ? {} : { name: `手动存档 ${new Date().toLocaleString("zh-CN")}` }),
    ...(discIndex === undefined ? {} : { discIndex }),
  };
  form.append("metadata", new Blob([JSON.stringify(metadata)], { type: "application/json" }));
  const stateBytes = new Uint8Array(payload.checkpoint.bytes).slice().buffer;
  form.append("payload", new Blob([stateBytes], { type: "application/octet-stream" }), "payload.bin");
  if (screenshot.screenshot.size > 0 && screenshot.screenshot.size <= maximumManualSaveScreenshotBytes) {
    form.append("screenshot", screenshot.screenshot, `screenshot.${screenshot.format || "png"}`);
  }
  return form;
}

function rejectSave(params: PlayerSessionParams, message: string) {
  params.setSyncText("保存失败"); params.setSyncTone("warning"); params.showToast(message, 4_000);
  return false;
}

async function waitForSaveUploadPresentation(startedAt: number) {
  const remaining = SAVE_UPLOAD_PRESENTATION_MS - (performance.now() - startedAt);
  if (remaining > 0) {await new Promise<void>((resolve) => window.setTimeout(resolve, remaining));}
}

export function waitForSaveUploadPresentationTurn() {
  return new Promise<void>((resolve) => window.setTimeout(resolve, 0));
}

function usePageHideFinish(params: PlayerSessionParams) {
  useEffect(() => {
    const finish = () => finishOnPageHide(params);
    window.addEventListener("pagehide", finish);
    return () => window.removeEventListener("pagehide", finish);
  }, [params]);
}

function usePageExitProtection(params: PlayerSessionParams) {
  useEffect(() => {
    const protect = (event: BeforeUnloadEvent) => {
      if (!params.started.current || params.finishing.current) {return;}
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", protect);
    return () => window.removeEventListener("beforeunload", protect);
  }, [params]);
}

function finishOnPageHide(params: PlayerSessionParams) {
  if (params.finishing.current) {return;}
  const wasStarted = params.started.current;
  beginPlayerFinish(params);
  void fetch(`/runtime/launches/${params.launchId}/finish`, { method: "POST", credentials: "same-origin", keepalive: true, headers: { "Content-Type": "application/json" }, body: JSON.stringify({ clientSequence: wasStarted ? params.sequence.current + 1 : 0, clientObservedAtMs: Date.now(), previousInterval: wasStarted ? { running: true, visible: document.visibilityState === "visible", paused: params.runtime.current?.getState() === "PAUSED" } : null }) });
}

function screenshotExtension(screenshot: Blob) {
  return screenshot.type === "image/jpeg" ? "jpg" : screenshot.type === "image/webp" ? "webp" : "png";
}
