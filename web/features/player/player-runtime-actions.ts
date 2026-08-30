"use client";

import type { Dispatch, RefObject, SetStateAction } from "react";
import { newUuid } from "@/lib/crypto";
import { writeHeaders } from "@/lib/api/client";
import { captureManualState, switchDiscPreservingPause, type DiscSet, type DiscState, type EmulatorInstance, type ManualScreenshot, type ManualStatePayload, type PlayerConfig } from "./adapters/ejs-4.2.3-v2";
import { closeEmulatorSettingsPanels, openEmulatorSettingsPanel, type EmulatorSettingsPanel } from "./emulator-settings";
import { multiDiscPlayerResultCode, type MultiDiscPlayerEvent } from "./multi-disc-telemetry";
import { applyVideoRenderingMode, writeVideoRenderingMode, type VideoRenderingMode } from "./video-rendering";

type Mutable<T> = { current: T };
type ShellState = "loading" | "running" | "error";
type SyncTone = "synced" | "busy" | "warning";

type RuntimeActionParams = {
  userId: string | undefined; state: ShellState; emulator: Mutable<EmulatorInstance | undefined>; frameRef: RefObject<HTMLIFrameElement | null>;
  manualSaveAvailableRef: Mutable<boolean>; pauseCapture: Mutable<Promise<ManualScreenshot | null>>; lastManualScreenshot: Mutable<ManualScreenshot | null>;
  uploadManualState: (payload: ManualStatePayload) => Promise<boolean>;
  discSetRef: Mutable<DiscSet | null>; discState: DiscState | null; setDiscState: Dispatch<SetStateAction<DiscState | null>>;
  reportPlayerEvent: (event: MultiDiscPlayerEvent) => void; showToast: (message: string, timeout?: number) => void;
  setSyncText: Dispatch<SetStateAction<string>>; setSyncTone: Dispatch<SetStateAction<SyncTone>>;
  setEmulatorToolbarOpen: Dispatch<SetStateAction<boolean>>; holdControls: () => void; releaseControls: () => void;
  lastAudibleVolume: Mutable<number>; emulatorVolume: number; emulatorMuted: boolean; setEmulatorVolume: Dispatch<SetStateAction<number>>; setEmulatorMuted: Dispatch<SetStateAction<boolean>>;
  videoRenderingModeRef: Mutable<VideoRenderingMode>; netplayConfig: Mutable<NonNullable<PlayerConfig["netplay"]> | null>;
  netplayPaused: boolean; netplayPausedRef: Mutable<boolean>; setNetplayPaused: Dispatch<SetStateAction<boolean>>;
};

export function usePlayerRuntimeActions(params: RuntimeActionParams) {
  async function saveManualState() {
    if (!params.manualSaveAvailableRef.current) {
      params.setSyncText("程序菜单模式不可存档"); params.setSyncTone("warning");
      params.showToast("请退出后从游戏详情选择一个具体 DOS 程序再开始；程序菜单模式无法创建可恢复存档。", 5_000);
      return false;
    }
    const current = params.emulator.current;
    if (!current) {return false;}
    params.setSyncText("正在保存…"); params.setSyncTone("busy"); params.showToast("正在创建存档…");
    try {
      const capture = await params.pauseCapture.current.catch(() => null) ?? params.lastManualScreenshot.current;
      return await params.uploadManualState(await captureManualState(current, capture));
    } catch {
      params.setSyncText("保存失败"); params.setSyncTone("warning"); params.showToast("无法从模拟器读取完整状态", 4_000);
      return false;
    }
  }

  async function selectDisc(index: number) {
    const current = params.emulator.current;
    const locked = params.discSetRef.current;
    if (!current || !locked || !params.discState) {return false;}
    if (index === params.discState.currentIndex) {return true;}
    try {
      const selected = switchDiscPreservingPause(current, index, locked.count);
      params.setDiscState(selected);
      params.reportPlayerEvent({ eventType: "SWITCH_SUCCESS", resultCode: "OK", discCount: locked.count, observedDiscCount: selected.count });
      params.showToast(`已切换到光盘 ${selected.currentIndex + 1}`);
      return true;
    } catch (caught) {
      params.reportPlayerEvent({ eventType: "SWITCH_FAILURE", resultCode: multiDiscPlayerResultCode(caught, "PLAYER_DISC_SWITCH_FAILED"), discCount: locked.count, observedDiscCount: observedRuntimeDiscCount(current) });
      params.showToast(`无法切换光盘，游戏仍停留在光盘 ${params.discState.currentIndex + 1}`, 4_000);
      return false;
    }
  }

  async function toggleFullscreen() {
    if (document.fullscreenElement) {await document.exitFullscreen().catch(() => params.showToast("浏览器未能退出全屏"));}
    else {await document.documentElement.requestFullscreen({ navigationUI: "hide" }).catch(() => params.showToast("浏览器未允许全屏，游戏仍会继续运行。", 4_000));}
  }

  function openEmulatorSettings() {
    if (!params.emulator.current || params.state !== "running") {params.showToast("模拟器设置尚未准备好，请稍后再试。", 3_000); return;}
    params.setEmulatorToolbarOpen(true); params.holdControls();
  }

  function closeEmulatorSettings() {
    closeEmulatorSettingsPanels(params.emulator.current); params.setEmulatorToolbarOpen(false); params.releaseControls();
  }

  function openEmulatorPanel(panel: EmulatorSettingsPanel) {
    if (!params.emulator.current || !openEmulatorSettingsPanel(params.emulator.current, panel)) {params.showToast("当前模拟器未提供这项设置。", 3_000); return;}
    params.holdControls();
  }

  function changeEmulatorVolume(volume: number) {
    const current = params.emulator.current;
    if (!current) {return;}
    applyVolume(current, Math.min(1, Math.max(0, volume)), params);
  }

  function toggleEmulatorMute() {
    const current = params.emulator.current;
    if (!current) {return;}
    if (params.emulatorMuted) {restoreVolume(current, params); return;}
    muteVolume(current, params);
  }

  function changeVideoRenderingMode(mode: VideoRenderingMode) {
    rememberVideoRenderingMode(params, mode);
    writeVideoRenderingMode(params.userId, mode);
    const canvas = params.emulator.current?.canvas ?? params.frameRef.current?.contentDocument?.querySelector<HTMLCanvasElement>("canvas") ?? null;
    const applied = applyVideoRenderingMode(params.emulator.current, canvas, mode);
    params.showToast(applied ? "画面模式已应用" : "画面模式将在模拟器准备完成后应用");
  }

  async function toggleNetplayPause() {
    const action = params.netplayPaused ? "resume" : "pause";
    if (!await requestNetplayPause(action)) {params.showToast("无法更改全局暂停状态，请重试。", 4_000);}
  }

  const requestNetplayPause = (action: "pause" | "resume") => requestGlobalPause(action, params);

  return { saveManualState, selectDisc, toggleFullscreen, openEmulatorSettings, closeEmulatorSettings, openEmulatorPanel, changeEmulatorVolume, toggleEmulatorMute, changeVideoRenderingMode, toggleNetplayPause, requestNetplayPause };
}

function applyVolume(current: EmulatorInstance, normalized: number, params: RuntimeActionParams) {
  current.volume = normalized; current.muted = normalized === 0; current.setVolume?.(normalized);
  params.setEmulatorVolume(normalized); params.setEmulatorMuted(normalized === 0);
  if (normalized > 0) {params.lastAudibleVolume.current = normalized;}
}

function muteVolume(current: EmulatorInstance, params: RuntimeActionParams) {
  if (params.emulatorVolume > 0) {params.lastAudibleVolume.current = params.emulatorVolume;}
  current.muted = true; current.setVolume?.(0); params.setEmulatorMuted(true);
}

function rememberVideoRenderingMode(params: RuntimeActionParams, mode: VideoRenderingMode) {
  params.videoRenderingModeRef.current = mode;
}

async function requestGlobalPause(action: "pause" | "resume", params: RuntimeActionParams) {
  const locked = params.netplayConfig.current;
  if (!locked || locked.playerNo !== 1) {return false;}
  const response = await fetch(`/api/v1/netplay/rooms/${locked.roomId}/sessions/${locked.sessionId}/${action}`, { method: "POST", credentials: "same-origin", headers: writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }), body: "{}" }).catch(() => null);
  if (!response?.ok) {return false;}
  params.netplayPausedRef.current = action === "pause";
  params.setNetplayPaused(action === "pause");
  return true;
}

function restoreVolume(current: EmulatorInstance, params: RuntimeActionParams) {
  const restored = Math.min(1, Math.max(0.01, params.lastAudibleVolume.current));
  current.volume = restored; current.muted = false; current.setVolume?.(restored);
  params.setEmulatorVolume(restored); params.setEmulatorMuted(false);
}

function observedRuntimeDiscCount(instance: EmulatorInstance | undefined) {
  const value = instance?.gameManager?.getDiskCount?.();
  return typeof value === "number" && Number.isInteger(value) && value >= -1 && value <= 64 ? value : null;
}
