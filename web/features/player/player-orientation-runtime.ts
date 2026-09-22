"use client";

import { useCallback, useEffect, type Dispatch, type SetStateAction } from "react";
import { observeStableOrientation, portraitPlayerQuery, reducePlayerOrientation, requestFullscreenAndLandscape, type PlayerOrientationEffect, type PlayerOrientationState } from "./orientation";
import type {PlayerRuntimeV1} from "./runtime/contract";

type Mutable<T> = { current: T };

type OrientationParams = {
  runtime: Mutable<PlayerRuntimeV1 | null>; pausedRef: Mutable<boolean>; orientationStateRef: Mutable<PlayerOrientationState>; setOrientationState: Dispatch<SetStateAction<PlayerOrientationState>>;
  setPaused: Dispatch<SetStateAction<boolean>>; setOrientationHelp: Dispatch<SetStateAction<string>>;
  showControls: () => void; showToast: (message: string, timeout?: number) => void;
};

export function usePlayerOrientationRuntime(params: OrientationParams) {
  const runEffects = useCallback(async (effects: PlayerOrientationEffect[]) => {
    const queue = [...effects];
    while (queue.length) {await runOrientationEffect(queue.shift(), params);}
  }, [params]);

  useEffect(() => {
    if (typeof window.matchMedia !== "function") {return;}
    const portraitQuery = window.matchMedia(portraitPlayerQuery);
    const apply = (portrait: boolean) => {
      const paused = params.pausedRef.current;
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

async function runOrientationEffect(effect: PlayerOrientationEffect | undefined, params: OrientationParams) {
  if (effect === "release-input") {releaseInput(); return;}
  if (effect === "pause-single") {await pauseSingle(params); return;}
  if (effect === "resume-single") {await resumeSingle(params); return;}
}

function releaseInput() {
  if (document.activeElement instanceof HTMLElement) {document.activeElement.blur();}
}

async function pauseSingle(params: OrientationParams) {
  if (!params.runtime.current?.getCapabilities().pause) {return;}
  await params.runtime.current.pause();
  params.pausedRef.current = true;
  params.setPaused(true);
}

async function resumeSingle(params: OrientationParams) {
  if (document.visibilityState !== "visible" || !params.runtime.current?.getCapabilities().pause) {return;}
  await params.runtime.current.resume();
  params.pausedRef.current = false;
  params.setPaused(false);
  params.showControls();
}

function applyTransition(transition: { state: PlayerOrientationState; effects: PlayerOrientationEffect[] }, params: OrientationParams, runEffects: (effects: PlayerOrientationEffect[]) => Promise<void>) {
  params.orientationStateRef.current = transition.state;
  params.setOrientationState(transition.state);
  void runEffects(transition.effects);
}
