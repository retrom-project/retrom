"use client";

import type {Dispatch, SetStateAction} from "react";
import type {EmulatorSettingsPanel} from "./emulator-settings";
import type {GameSaveSync} from "./game-save-sync";
import {multiDiscPlayerResultCode, type MultiDiscPlayerEvent} from "./multi-disc-telemetry";
import type {LaunchEnvelopeV1, PlayerRuntimeV2, RuntimeDiscStateV1} from "./runtime/contract";
import {captureRuntimeSave, setRuntimeVideoMode, setRuntimeVolume, switchRuntimeDisc, type RuntimeSavePayload} from "./runtime/runtime-actions";
import {writeVideoRenderingMode, type VideoRenderingMode} from "./video-rendering";

type Mutable<T> = {current: T};
type ShellState = "loading" | "running" | "error";
type SyncTone = "synced" | "busy" | "warning";

type RuntimeActionParams = {
  userId: string | undefined;
  state: ShellState;
  runtime: Mutable<PlayerRuntimeV2 | null>;
  envelope: Mutable<LaunchEnvelopeV1 | null>;
  manualSaveAvailableRef: Mutable<boolean>;
  programSelectionRequiredRef: Mutable<boolean>;
  uploadManualState: (payload: RuntimeSavePayload) => Promise<boolean>;
  gameSaveSync?: Mutable<GameSaveSync | null>;
  discState: RuntimeDiscStateV1 | null;
  setDiscState: Dispatch<SetStateAction<RuntimeDiscStateV1 | null>>;
  reportPlayerEvent: (event: MultiDiscPlayerEvent) => void;
  showToast: (message: string, timeout?: number) => void;
  setSyncText: Dispatch<SetStateAction<string>>;
  setSyncTone: Dispatch<SetStateAction<SyncTone>>;
  setEmulatorToolbarOpen: Dispatch<SetStateAction<boolean>>;
  holdControls: () => void;
  releaseControls: () => void;
  lastAudibleVolume: Mutable<number>;
  emulatorVolume: number;
  emulatorMuted: boolean;
  setEmulatorVolume: Dispatch<SetStateAction<number>>;
  setEmulatorMuted: Dispatch<SetStateAction<boolean>>;
  videoRenderingModeRef: Mutable<VideoRenderingMode>;
  };

export function usePlayerRuntimeActions(params: RuntimeActionParams) {
  async function saveManualState() {
    if (params.gameSaveSync?.current) {return params.gameSaveSync.current.capture();}
    if (!params.manualSaveAvailableRef.current) {
      params.setSyncText(params.programSelectionRequiredRef.current ? "程序菜单模式不可存档" : "当前场景暂不可存档");
      params.setSyncTone("warning");
      params.showToast(params.programSelectionRequiredRef.current
        ? "请退出后从游戏详情选择一个启动程序再开始；程序菜单模式无法创建可恢复存档。"
        : "当前游戏状态暂时无法创建可恢复存档，请继续游戏后重试。", 5_000);
      return false;
    }
    const runtime = params.runtime.current;
    if (!runtime) {return false;}
    params.setSyncText("正在保存…"); params.setSyncTone("busy"); params.showToast("正在创建存档…");
    try {return await params.uploadManualState(await captureRuntimeSave(runtime));}
    catch {params.setSyncText("保存失败"); params.setSyncTone("warning"); params.showToast("无法从运行时读取完整状态", 4_000); return false;}
  }

  async function selectDisc(index: number) {
    const runtime = params.runtime.current;
    const current = params.discState;
    if (!runtime || !current || index === current.currentIndex) {return Boolean(current);}
    try {
      const selected = await switchRuntimeDisc(runtime, index);
      params.setDiscState(selected);
      params.reportPlayerEvent({eventType: "SWITCH_SUCCESS", resultCode: "OK", discCount: selected.count, observedDiscCount: selected.count});
      params.showToast(`已切换到光盘 ${selected.currentIndex + 1}`);
      return true;
    } catch (caught) {
      params.reportPlayerEvent({eventType: "SWITCH_FAILURE", resultCode: multiDiscPlayerResultCode(caught, "PLAYER_DISC_SWITCH_FAILED"), discCount: current.count, observedDiscCount: current.count});
      params.showToast(`无法切换光盘，游戏仍停留在光盘 ${current.currentIndex + 1}`, 4_000);
      return false;
    }
  }

  async function toggleFullscreen() {
    if (document.fullscreenElement) {await document.exitFullscreen().catch(() => params.showToast("浏览器未能退出全屏"));}
    else {await document.documentElement.requestFullscreen({navigationUI: "hide"}).catch(() => params.showToast("浏览器未允许全屏，游戏仍会继续运行。", 4_000));}
  }

  function openEmulatorSettings() {
    if (!params.runtime.current || params.state !== "running") {params.showToast("模拟器设置尚未准备好，请稍后再试。", 3_000); return;}
    params.setEmulatorToolbarOpen(true); params.holdControls();
  }

  async function closeEmulatorSettings() {
    if (!await openEmulatorPanel(null)) {return false;}
    params.setEmulatorToolbarOpen(false); params.releaseControls();
    return true;
  }

  async function openEmulatorPanel(panel: EmulatorSettingsPanel | null) {
    const runtime = params.runtime.current;
    if (!runtime?.getCapabilities().nativeSettings) {
      if (panel === null) {return true;}
      params.showToast("当前运行时未提供这项设置。", 3_000);
      return false;
    }
    params.holdControls();
    try {
      if (panel === null) {await runtime.closeNativeSettings();}
      else {await runtime.openNativeSettings(panel);}
      params.showToast("");
      return true;
    } catch {
      params.showToast(panel === null ? "无法关闭运行时设置，请重试。" : "无法打开运行时设置，请重试。", 3_000);
      return false;
    }
  }

  function changeEmulatorVolume(volume: number) {
    const runtime = params.runtime.current;
    if (!runtime?.getCapabilities().volume) {return;}
    const normalized = Math.min(1, Math.max(0, volume));
    void setRuntimeVolume(runtime, normalized);
    params.setEmulatorVolume(normalized); params.setEmulatorMuted(normalized === 0);
    if (normalized > 0) {params.lastAudibleVolume.current = normalized;}
  }

  function toggleEmulatorMute() {
    if (params.emulatorMuted) {changeEmulatorVolume(Math.min(1, Math.max(0.01, params.lastAudibleVolume.current))); return;}
    if (params.emulatorVolume > 0) {params.lastAudibleVolume.current = params.emulatorVolume;}
    changeEmulatorVolume(0);
  }

  function changeVideoRenderingMode(mode: VideoRenderingMode) {
    const runtime = params.runtime.current;
    if (!runtime?.getCapabilities().videoModes.includes(mode)) {params.showToast("当前运行时不支持这项画面模式"); return;}
    void setRuntimeVideoMode(runtime, mode).then(() => {
      params.videoRenderingModeRef.current = mode;
      writeVideoRenderingMode(params.userId, mode);
      params.showToast("画面模式已应用");
    }).catch(() => params.showToast("无法应用画面模式，请重试。", 3_000));
  }


  return {saveManualState, selectDisc, toggleFullscreen, openEmulatorSettings, closeEmulatorSettings,
    openEmulatorPanel, changeEmulatorVolume, toggleEmulatorMute, changeVideoRenderingMode,
  };
}
