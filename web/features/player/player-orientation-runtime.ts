"use client";

import { useCallback, useEffect, type Dispatch, type RefObject, type SetStateAction } from "react";
import { setEmulatorPaused } from "./pause-control";
import { observeStableOrientation, portraitPlayerQuery, reducePlayerOrientation, requestFullscreenAndLandscape, type PlayerOrientationEffect, type PlayerOrientationState } from "./orientation";
import type { EmulatorInstance, PlayerConfig } from "./adapters/ejs-4.2.3-v2";
import type { NetplayController } from "./netplay/controller";

type Mutable<T> = { current: T };

type OrientationParams = {
  frameRef: RefObject<HTMLIFrameElement | null>; playerMode: Mutable<PlayerConfig["mode"]>; netplayController: Mutable<NetplayController | null>;
  emulator: Mutable<EmulatorInstance | undefined>; pausedRef: Mutable<boolean>; netplayPausedRef: Mutable<boolean>;
  orientationStateRef: Mutable<PlayerOrientationState>; setOrientationState: Dispatch<SetStateAction<PlayerOrientationState>>;
  setPaused: Dispatch<SetStateAction<boolean>>; setOrientationHelp: Dispatch<SetStateAction<string>>;
  requestNetplayPause: (action: "pause" | "resume") => Promise<boolean>; showControls: () => void; showToast: (message: string, timeout?: number) => void;
};

export function usePlayerOrientationRuntime(params: OrientationParams) {
  const runEffects = useCallback(async (effects: PlayerOrientationEffect[]) => {
    const queue = [...effects];
    while (queue.length) {await runOrientationEffect(queue.shift(), queue, params);}
  }, [params]);

  useEffect(() => {
    if (typeof window.matchMedia !== "function") {return;}
    const portraitQuery = window.matchMedia(portraitPlayerQuery);
    const apply = (portrait: boolean) => {
      const paused = params.playerMode.current === "single" ? params.pausedRef.current : params.netplayPausedRef.current;
      applyTransition(reducePlayerOrientation(params.orientationStateRef.current, { type: "orientation-stable", portrait, paused }), params, runEffects);
    };
    return observeStableOrientation(portraitQuery, apply);
  }, [params, runEffects]);

  useEffect(() => {
    const update = () => applyTransition(reducePlayerOrientation(params.orientationStateRef.current, { type: "visibility", hidden: document.visibilityState === "hidden" }), params, runEffects);
    document.addEventListener("visibilitychange", update);
    return () => document.removeEventListener("visibilitychange", update);
  }, [params, runEffects]);

  async function retryLandscape() {
    const result = await requestFullscreenAndLandscape();
    if (result.orientation === "unsupported") {params.setOrientationHelp("当前浏览器不支持自动锁定方向，请手动旋转设备。");}
    else if (result.orientation === "denied") {params.setOrientationHelp("浏览器拒绝了方向锁定，请手动旋转设备。");}
    else {params.setOrientationHelp("方向已锁定；若画面没有变化，请手动旋转设备。");}
  }

  return { retryLandscape };
}

async function runOrientationEffect(effect: PlayerOrientationEffect | undefined, queue: PlayerOrientationEffect[], params: OrientationParams) {
  if (effect === "release-input") {releaseInput(params); return;}
  if (effect === "pause-single") {pauseSingle(params); return;}
  if (effect === "resume-single") {resumeSingle(params); return;}
  if (effect === "pause-netplay") {await pauseNetplay(queue, params); return;}
  if (effect === "resume-netplay") {if (!await params.requestNetplayPause("resume")) {params.showToast("无法自动恢复联机，请由房主手动继续。", 4_000);} return;}
  if (effect === "warn-netplay-p2") {params.showToast("本局仍在进行，请立即横屏。", 4_000);}
}

function releaseInput(params: OrientationParams) {
  params.frameRef.current?.blur();
  params.frameRef.current?.contentWindow?.dispatchEvent(new Event("blur"));
  if (params.playerMode.current === "netplay") {params.netplayController.current?.handleFocusLoss();}
}

function pauseSingle(params: OrientationParams) {
  if (!setEmulatorPaused(params.emulator.current, true)) {return;}
  params.pausedRef.current = true;
  params.setPaused(true);
}

function resumeSingle(params: OrientationParams) {
  if (document.visibilityState !== "visible" || !setEmulatorPaused(params.emulator.current, false)) {return;}
  params.pausedRef.current = false;
  params.setPaused(false);
  params.showControls();
}

async function pauseNetplay(queue: PlayerOrientationEffect[], params: OrientationParams) {
  if (!await params.requestNetplayPause("pause")) {params.showToast("无法在旋转时暂停联机，请立即横屏并手动确认状态。", 4_000); return;}
  const transition = reducePlayerOrientation(params.orientationStateRef.current, { type: "netplay-pause-owned" });
  params.orientationStateRef.current = transition.state;
  params.setOrientationState(transition.state);
  queue.unshift(...transition.effects);
}

function applyTransition(transition: { state: PlayerOrientationState; effects: PlayerOrientationEffect[] }, params: OrientationParams, runEffects: (effects: PlayerOrientationEffect[]) => Promise<void>) {
  params.orientationStateRef.current = transition.state;
  params.setOrientationState(transition.state);
  void runEffects(transition.effects);
}
