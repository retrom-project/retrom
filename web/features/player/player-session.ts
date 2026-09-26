"use client";

import { useCallback, useEffect, type Dispatch, type SetStateAction } from "react";
import { newUuid } from "@/lib/crypto";
import type {LaunchEnvelopeV1, PlayerRuntimeV1} from "./runtime/contract";
import type {RuntimeSavePayload} from "./runtime/runtime-actions";
import { uploadWithRestartRetry, type SaveUploadProgress } from "./upload-with-progress";
import { maximumManualSaveScreenshotBytes, prepareManualSaveScreenshot } from "./manual-save-screenshot";
import { reducePlayerOrientation, unlockLandscape, type PlayerOrientationState } from "./orientation";
import {saveReviewScreenshot} from "./review-preview-screenshot";
import {GameSaveConflict, isGameSaveConflict} from "./game-save-upload-error";
import {notifyReviewCheckpoint} from "./review-preview-receipt";
import type {PlayProgressClock} from "./play-progress-clock";

const SAVE_UPLOAD_PRESENTATION_MS = 400;
const SAVE_UPLOAD_TIMEOUT_MS = 300_000;
type Mutable<T> = { current: T };
type SyncTone = "synced" | "busy" | "warning";

export type PlayerSessionParams = {
  launchId: string; runtime: Mutable<PlayerRuntimeV1 | null>; envelope: Mutable<LaunchEnvelopeV1 | null>;
  progressClock: Mutable<PlayProgressClock>; started: Mutable<boolean>; finishing: Mutable<boolean>;
  heartbeat: Mutable<number | null>; saveUploadQueue: Mutable<Promise<void>>;
  orientationStateRef: Mutable<PlayerOrientationState>; returnTo: Mutable<string>;
  replaceImmersiveRoute: (url: string) => void;
  setOrientationState: Dispatch<SetStateAction<PlayerOrientationState>>; setSaveUploadProgress: Dispatch<SetStateAction<number | null>>;
  setSyncText: Dispatch<SetStateAction<string>>; setSyncTone: Dispatch<SetStateAction<SyncTone>>;
  showToast: (message: string, timeout?: number) => void;
};

export function usePlayerSession(params: PlayerSessionParams) {
  const reportProgress = useCallback(() => sendPlayProgress(params), [params]);

  const reportSaveUploadProgress = useCallback((progress: SaveUploadProgress) => {
    params.setSaveUploadProgress(progress.percent);
    params.setSyncText(`正在上传存档 ${progress.percent}%`);
    params.setSyncTone("busy");
  }, [params]);

  const uploadManualState = useCallback(async (payload: RuntimeSavePayload) => Boolean(await queueStateUpload(payload, params, reportSaveUploadProgress)), [params, reportSaveUploadProgress]);

  const captureReviewScreenshot = useCallback(() => queueReviewScreenshot(params), [params]);

  const exit = useCallback(() => exitPlayer(params, reportProgress), [params, reportProgress]);
  const exitStrict = useCallback(() => exitImmersivePlayer(params, reportProgress), [params, reportProgress]);
  const exitImmersiveAfterRuntimeExit = useCallback(
    () => exitImmersivePlayer(params, reportProgress),
    [params, reportProgress],
  );

  usePageHideFinish(params);
  usePageExitProtection(params);
  useProgressVisibility(params);
  return { reportProgress, uploadManualState, captureReviewScreenshot, exit, exitStrict, exitImmersiveAfterRuntimeExit };
}

async function queueReviewScreenshot(params: PlayerSessionParams) {
    if (params.envelope.current?.session.purpose !== "REVIEW_PREVIEW" || params.finishing.current) {return;}
    const result = params.saveUploadQueue.current.then(async () => {
      if (!params.runtime.current) {throw new Error("PLAYER_RUNTIME_UNAVAILABLE");}
      await saveReviewScreenshot(params.runtime.current, params.launchId);
      params.showToast("审核截图已保存");
    });
    params.saveUploadQueue.current = result.catch(() => {params.showToast("审核截图保存失败，请重试", 4_000);});
    await params.saveUploadQueue.current;
}

async function sendPlayProgress(params: PlayerSessionParams, keepalive = false): Promise<void> {
  if (!params.started.current || params.envelope.current?.session.purpose !== "PRODUCT") {return;}
  try {
    const response = await fetch(`/runtime/launches/${params.launchId}/progress`, {
      method: "POST", credentials: "same-origin", keepalive,
      signal: keepalive ? undefined : AbortSignal.timeout(5_000),
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({activeDurationMs: params.progressClock.current.snapshot(performance.now())}),
    });
    // Fetch resolves on headers; drain the small response before its timeout fires.
    await response.text();
  } catch { /* Telemetry never controls the runtime or navigation. */ }
}

function beginPlayerFinish(params: PlayerSessionParams) {
  params.finishing.current = true;
  params.progressClock.current.stop(performance.now());
  if (params.heartbeat.current !== null) {
    window.clearInterval(params.heartbeat.current);
    params.heartbeat.current = null;
  }
}

function queueStateUpload(payload: RuntimeSavePayload, params: PlayerSessionParams, reportProgress: (progress: SaveUploadProgress) => void) {
  const result = params.saveUploadQueue.current.then(() => uploadState(payload, params, reportProgress));
  params.saveUploadQueue.current = result.then(() => undefined, () => undefined);
  return result.catch((error: unknown) => {
    if (error instanceof GameSaveConflict) {throw error;}
    params.setSaveUploadProgress(null); params.setSyncText("保存失败"); params.setSyncTone("warning");
    params.showToast(payload.source === "GAME_SAVE" ? "游戏数据保存失败，本地草稿已保留" :
      "手动存档上传失败，服务器未创建不完整记录", 4_000);
    return false;
  });
}

async function exitPlayer(params: PlayerSessionParams, reportProgress: () => Promise<void>) {
  if (params.finishing.current) {return;}
  beginPlayerFinish(params);
  const exiting = reducePlayerOrientation(params.orientationStateRef.current, { type: "exit" });
  params.orientationStateRef.current = exiting.state;
  params.setOrientationState(exiting.state);
  if (exiting.effects.includes("unlock")) {unlockLandscape();}
  await params.saveUploadQueue.current;
  void reportProgress();
  finishPreview(params);
  if (document.fullscreenElement) {await document.exitFullscreen().catch(() => undefined);}
  if (params.envelope.current?.session.purpose === "REVIEW_PREVIEW" && window.opener) {
    window.close();
    if (window.closed) {return;}
  }
  window.location.replace(params.returnTo.current);
}

async function exitImmersivePlayer(
  params: PlayerSessionParams,
  reportProgress: () => Promise<void>,
) {
  if (params.finishing.current) {return;}
  const exiting = reducePlayerOrientation(params.orientationStateRef.current, { type: "exit" });
  params.orientationStateRef.current = exiting.state;
  params.setOrientationState(exiting.state);
  if (exiting.effects.includes("unlock")) {unlockLandscape();}
  beginPlayerFinish(params);
  void reportProgress();
  finishPreview(params);
  params.replaceImmersiveRoute(params.returnTo.current);
}

async function uploadState(payload: RuntimeSavePayload, params: PlayerSessionParams, reportProgress: (progress: SaveUploadProgress) => void) {
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
  const form = createSaveForm(payload, uploadPayload, discIndex);
  const startedAt = performance.now();
  params.setSaveUploadProgress(0);
  await waitForSaveUploadPresentationTurn();
  let response: Awaited<ReturnType<typeof uploadWithRestartRetry>>;
  try {
    response = await uploadWithRestartRetry({
      url: `/runtime/launches/${params.launchId}/save-states`, method: "POST",
      headers: { "Idempotency-Key": payload.requestId ?? newUuid() }, body: form,
      totalBytes: payload.checkpoint.bytes.byteLength + uploadPayload.screenshot.size,
      timeoutMs: SAVE_UPLOAD_TIMEOUT_MS, onProgress: reportProgress,
    });
  } finally {
    await waitForSaveUploadPresentation(startedAt);
    params.setSaveUploadProgress(null);
  }
  return finishStateUpload(response, payload, uploadPayload.screenshot.size, params);
}

function finishStateUpload(
  response: Awaited<ReturnType<typeof uploadWithRestartRetry>>,
  payload: RuntimeSavePayload,
  screenshotSize: number,
  params: PlayerSessionParams,
) {
  if (payload.source === "GAME_SAVE" && response.status === 409 && isGameSaveConflict(response.body)) {throw new GameSaveConflict();}
  if (!response.ok) {return rejectSave(params, payload.source === "GAME_SAVE"
    ? "游戏数据保存失败，本地草稿已保留" : "手动存档失败，服务器未创建不完整记录");}
  if (params.envelope.current?.session.purpose === "REVIEW_PREVIEW") {
    notifyReviewCheckpoint(response.body, params.launchId, params.returnTo.current);
  }
  params.setSyncText("已同步"); params.setSyncTone("synced");
  params.showToast(payload.source === "GAME_SAVE" ? "游戏数据已保存" :
    screenshotSize ? "手动存档和截图已保存" : "手动存档已保存，未附带截图");
  return true;
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
) {
  const form = new FormData();
  const metadata = {
    checkpointFormat: payload.checkpoint.format,
    name: payload.source === "GAME_SAVE" ? payload.name ?? "游戏内存档" : `手动存档 ${new Date().toLocaleString("zh-CN")}`,
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

function useProgressVisibility(params: PlayerSessionParams) {
  useEffect(() => {
    const update = () => {
      const visible = document.visibilityState === "visible";
      params.progressClock.current.setVisible(performance.now(), visible);
      if (visible) {void sendPlayProgress(params);}
    };
    document.addEventListener("visibilitychange", update);
    return () => document.removeEventListener("visibilitychange", update);
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
  beginPlayerFinish(params);
  void sendPlayProgress(params, true);
  finishPreview(params);
}

function finishPreview(params: PlayerSessionParams) {
  if (params.envelope.current?.session.purpose !== "REVIEW_PREVIEW") {return;}
  void fetch(`/runtime/launches/${params.launchId}/finish`, {
    method: "POST", credentials: "same-origin", keepalive: true,
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({clientSequence: 0, clientObservedAtMs: Date.now(), previousInterval: null}),
  }).catch(() => undefined);
}

function screenshotExtension(screenshot: Blob) {
  return screenshot.type === "image/jpeg" ? "jpg" : screenshot.type === "image/webp" ? "webp" : "png";
}
