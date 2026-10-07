"use client";
import { useEffect, useEffectEvent, useRef, useState, type RefObject } from "react";
import type { PlayerRuntimeV1, RuntimeStateV1, RuntimeVideoModeV1 } from "./runtime/contract";
import type { EmulatorSettingsPanel } from "./player-settings";
export function usePlayerSettings(runtime: RefObject<PlayerRuntimeV1 | null>, state: RuntimeStateV1, onError: (message: string) => void) {
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<RuntimeVideoModeV1>("pixel");
  const applied = useRef<PlayerRuntimeV1 | null>(null);
  const report = useEffectEvent(onError);
  useEffect(() => {
    const instance = runtime.current;
    if (!instance || !["RUNNING", "PAUSED"].includes(state) || applied.current === instance) { return; }
    const modes = instance.getCapabilities().videoModes;
    if (!modes.length) { return; }
    applied.current = instance;
    const selected = modes.includes("pixel") ? "pixel" : modes[0];
    void instance.setVideoMode(selected).then(() => setMode(selected), () => report("无法设置画面模式。"));
  }, [runtime, state]);
  async function show() {
    const instance = runtime.current;
    if (!instance || !["RUNNING", "PAUSED"].includes(instance.getState())) { return; }
    try {
      if (instance.getState() === "RUNNING" && instance.getCapabilities().pause) { await instance.pause(); }
      setOpen(true);
    } catch (failure) { onError(failure instanceof Error ? failure.message : "无法打开模拟器设置。"); }
  }
  async function navigate(panel: EmulatorSettingsPanel | null | "close") {
    try {
      const instance = runtime.current;
      if (!instance) { return false; }
      if (panel && panel !== "close") { await instance.openNativeSettings(panel); }
      else if (instance.getCapabilities().nativeSettings) { await instance.closeNativeSettings(); }
      if (panel === "close") { setOpen(false); }
      return true;
    } catch (failure) { onError(failure instanceof Error ? failure.message : "无法切换模拟器设置。"); return false; }
  }
  async function changeVideo(value: RuntimeVideoModeV1) {
    try { await runtime.current?.setVideoMode(value); setMode(value); }
    catch (failure) { onError(failure instanceof Error ? failure.message : "无法调整画面。"); }
  }
  return { open, mode, show, navigate, changeVideo };
}
