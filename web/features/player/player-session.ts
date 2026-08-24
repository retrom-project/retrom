"use client";

import { useCallback, useEffect, type Dispatch, type SetStateAction } from "react";
import { newUuid } from "@/lib/crypto";
import { readDiscState, type DiscSet, type EmulatorInstance, type PlayerConfig } from "./adapters/ejs-4.2.3-v2";
import { uploadWithProgress, type SaveUploadProgress } from "./upload-with-progress";
import { reducePlayerOrientation, unlockLandscape, type PlayerOrientationState } from "./orientation";
import type { NetplayController } from "./netplay/controller";

const SAVE_UPLOAD_PRESENTATION_MS = 400;
type Mutable<T> = { current: T };
type SyncTone = "synced" | "busy" | "warning";

type PlayerSessionParams = {
  launchId: string; emulator: Mutable<EmulatorInstance | undefined>; playerMode: Mutable<PlayerConfig["mode"]>;
  sequence: Mutable<number>; started: Mutable<boolean>; finishing: Mutable<boolean>; saveUploadQueue: Mutable<Promise<void>>;
  discSetRef: Mutable<DiscSet | null>; orientationStateRef: Mutable<PlayerOrientationState>; returnTo: Mutable<string>;
  netplayController: Mutable<NetplayController | null>;
  setOrientationState: Dispatch<SetStateAction<PlayerOrientationState>>; setSaveUploadProgress: Dispatch<SetStateAction<number | null>>;
  setSyncText: Dispatch<SetStateAction<string>>; setSyncTone: Dispatch<SetStateAction<SyncTone>>;
  showToast: (message: string, timeout?: number) => void;
};

export function usePlayerSession(params: PlayerSessionParams) {
  const sendEvent = useCallback((kind: "start" | "heartbeat" | "finish") => sendPlayerEvent(kind, params), [params]);

  const reportProgress = useCallback((progress: SaveUploadProgress) => {
    params.setSaveUploadProgress(progress.percent);
    params.setSyncText(`正在上传存档 ${progress.percent}%`);
    params.setSyncTone("busy");
  }, [params]);

  const uploadManualState = useCallback((payload: { screenshot: Blob; format: string; state: Uint8Array }) => queueManualState(payload, params, reportProgress), [params, reportProgress]);

  const exit = useCallback(() => exitPlayer(params, sendEvent), [params, sendEvent]);
  const exitStrict = useCallback(() => exitImmersivePlayer(params, sendEvent), [params, sendEvent]);

  usePageHideFinish(params);
  return { sendEvent, uploadManualState, exit, exitStrict };
}

async function sendPlayerEvent(kind: "start" | "heartbeat" | "finish", params: PlayerSessionParams) {
  if (kind === "heartbeat" && !params.started.current) {throw new Error("PLAY_SESSION_NOT_STARTED");}
  const firstOrUnstartedFinish = kind === "start" || kind === "finish" && !params.started.current;
  const next = firstOrUnstartedFinish ? 0 : params.sequence.current + 1;
  const response = await fetch(`/runtime/launches/${params.launchId}/${kind}`, { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ clientSequence: next, clientObservedAtMs: Date.now(), previousInterval: firstOrUnstartedFinish ? null : { running: true, visible: document.visibilityState === "visible", paused: params.emulator.current?.paused === true } }) });
  if (!response.ok) {throw new Error("PLAY_SESSION_EVENT_FAILED");}
  params.sequence.current = next;
  if (kind === "start") {params.started.current = true;}
  if (kind === "finish") {params.finishing.current = true;}
}

function queueManualState(payload: { screenshot: Blob; format: string; state: Uint8Array }, params: PlayerSessionParams, reportProgress: (progress: SaveUploadProgress) => void) {
  const result = params.saveUploadQueue.current.then(() => uploadState(payload, params, reportProgress));
  params.saveUploadQueue.current = result.then(() => undefined, () => undefined);
  return result.catch(() => {
    params.setSaveUploadProgress(null); params.setSyncText("保存失败"); params.setSyncTone("warning");
    params.showToast("手动存档上传失败，服务器未创建不完整记录", 4_000);
    return false;
  });
}

async function exitPlayer(params: PlayerSessionParams, sendEvent: (kind: "start" | "heartbeat" | "finish") => Promise<void>) {
  if (params.finishing.current) {return;}
  params.finishing.current = true;
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
) {
  if (params.finishing.current) {return;}
  const exiting = reducePlayerOrientation(params.orientationStateRef.current, { type: "exit" });
  params.orientationStateRef.current = exiting.state;
  params.setOrientationState(exiting.state);
  if (exiting.effects.includes("unlock")) {unlockLandscape();}
  await sendEvent("finish");
  if (document.fullscreenElement) {await document.exitFullscreen().catch(() => undefined);}
  window.location.replace(params.returnTo.current);
}

async function uploadState(payload: { screenshot: Blob; format: string; state: Uint8Array }, params: PlayerSessionParams, reportProgress: (progress: SaveUploadProgress) => void) {
  if (!payload.screenshot.size || !payload.state.byteLength) {return rejectSave(params, "状态或截图为空，未创建存档。");}
  const discIndex = currentDiscIndex(params);
  if (discIndex === "unavailable") {return rejectSave(params, "无法读取当前光盘，未创建存档。");}
  const form = createSaveForm(payload, discIndex);
  const startedAt = performance.now();
  params.setSaveUploadProgress(0);
  await new Promise<void>((resolve) => window.requestAnimationFrame(() => resolve()));
  let response: Awaited<ReturnType<typeof uploadWithProgress>>;
  try {
    response = await uploadWithProgress({ url: `/runtime/launches/${params.launchId}/save-states`, method: "POST", headers: { "Idempotency-Key": newUuid() }, body: form, totalBytes: payload.state.byteLength + payload.screenshot.size, onProgress: reportProgress });
  } finally {
    await waitForSaveUploadPresentation(startedAt);
    params.setSaveUploadProgress(null);
  }
  if (!response.ok) {return rejectSave(params, "手动存档失败，服务器未创建不完整记录");}
  params.setSyncText("已同步"); params.setSyncTone("synced"); params.showToast("手动存档和截图已保存");
  return true;
}

function currentDiscIndex(params: PlayerSessionParams): number | undefined | "unavailable" {
  if (!params.discSetRef.current) {return undefined;}
  try {
    if (!params.emulator.current) {return "unavailable";}
    return readDiscState(params.emulator.current, params.discSetRef.current.count).currentIndex;
  } catch {return "unavailable";}
}

function createSaveForm(payload: { screenshot: Blob; format: string; state: Uint8Array }, discIndex: number | undefined) {
  const form = new FormData();
  form.append("metadata", new Blob([JSON.stringify({ name: `手动存档 ${new Date().toLocaleString("zh-CN")}`, ...(discIndex === undefined ? {} : { discIndex }) })], { type: "application/json" }));
  const stateBytes = new Uint8Array(payload.state).slice().buffer;
  form.append("state", new Blob([stateBytes], { type: "application/octet-stream" }), `state.${payload.format || "bin"}`);
  form.append("screenshot", payload.screenshot, `screenshot.${payload.format || "png"}`);
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

function usePageHideFinish(params: PlayerSessionParams) {
  useEffect(() => {
    const finish = () => finishOnPageHide(params);
    window.addEventListener("pagehide", finish);
    return () => window.removeEventListener("pagehide", finish);
  }, [params]);
}

function finishOnPageHide(params: PlayerSessionParams) {
  if (params.finishing.current) {return;}
  const wasStarted = params.started.current;
  void fetch(`/runtime/launches/${params.launchId}/finish`, { method: "POST", credentials: "same-origin", keepalive: true, headers: { "Content-Type": "application/json" }, body: JSON.stringify({ clientSequence: wasStarted ? params.sequence.current + 1 : 0, clientObservedAtMs: Date.now(), previousInterval: wasStarted ? { running: true, visible: document.visibilityState === "visible", paused: params.emulator.current?.paused === true } : null }) });
  params.finishing.current = true;
}
