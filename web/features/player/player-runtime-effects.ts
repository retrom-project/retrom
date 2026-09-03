"use client";

import { useEffect, type Dispatch, type RefObject, type SetStateAction } from "react";
import type {PlayerRuntimeV1} from "./runtime/contract";
import { samplePlayerDebugMetrics, type PlayerDebugMetrics, type PlayerDebugSample } from "./player-debug";
import type { NetplayController } from "./netplay/controller";
import { shouldAutoHidePlayerControls } from "./player-controls-visibility";

type Mutable<T> = { current: T };

type RuntimeEffectParams = {
  state: "loading" | "running" | "error"; debugOpen: boolean; orientationBlocked: boolean;
  runtime: Mutable<PlayerRuntimeV1 | null>; orientationButtonRef: RefObject<HTMLButtonElement | null>;
  running: Mutable<boolean>; pausedRef: Mutable<boolean>; chromePinned: Mutable<boolean>; controlsTimer: Mutable<number | null>;
  playerMode: Mutable<"single" | "netplay">; netplayController: Mutable<NetplayController | null>;
  clearControlsTimer: () => void; setControlsVisible: Dispatch<SetStateAction<boolean>>; setFullscreen: Dispatch<SetStateAction<boolean>>;
  setDebugOpen: Dispatch<SetStateAction<boolean>>; setDebugMetrics: Dispatch<SetStateAction<PlayerDebugMetrics | null>>;
};

export function usePlayerRuntimeEffects(params: RuntimeEffectParams) {
  useEffect(() => {
    updateRunningState(params);
    return params.clearControlsTimer;
  }, [params]);

  useEffect(() => {
    const update = () => params.setFullscreen(document.fullscreenElement !== null);
    update();
    document.addEventListener("fullscreenchange", update);
    return () => document.removeEventListener("fullscreenchange", update);
  }, [params]);

  useEffect(() => {
    if (!params.orientationBlocked) {return;}
    const frame = requestAnimationFrame(() => params.orientationButtonRef.current?.focus());
    return () => cancelAnimationFrame(frame);
  }, [params]);

  useEffect(() => {
    if (!params.debugOpen) {return;}
    let previous: PlayerDebugSample | null = null;
    const sample = () => {
      const canvas = params.runtime.current?.getCanvas() ?? null;
      const result = samplePlayerDebugMetrics(params.runtime.current, canvas, previous, performance.now(), { width: window.innerWidth, height: window.innerHeight, devicePixelRatio: window.devicePixelRatio });
      previous = result.sample;
      params.setDebugMetrics(result.metrics);
    };
    const initialFrame = window.requestAnimationFrame(sample);
    const timer = window.setInterval(sample, 1_000);
    window.addEventListener("resize", sample);
    return () => {window.cancelAnimationFrame(initialFrame); window.clearInterval(timer); window.removeEventListener("resize", sample);};
  }, [params]);

  useEffect(() => {
    const releaseHidden = () => {if (document.visibilityState === "hidden" && params.playerMode.current === "netplay") {params.netplayController.current?.handleFocusLoss();}};
    const releaseBlurred = () => {if (params.playerMode.current === "netplay") {params.netplayController.current?.handleFocusLoss();}};
    document.addEventListener("visibilitychange", releaseHidden);
    window.addEventListener("blur", releaseBlurred);
    return () => {document.removeEventListener("visibilitychange", releaseHidden); window.removeEventListener("blur", releaseBlurred);};
  }, [params]);

  function toggleDebug() {
    if (!params.debugOpen) {params.setDebugMetrics(null);}
    params.setDebugOpen((open) => !open);
  }

  return { toggleDebug };
}

function updateRunningState(params: RuntimeEffectParams) {
  params.running.current = params.state === "running";
  params.clearControlsTimer();
  params.setControlsVisible(!shouldAutoHidePlayerControls(params.state, params.pausedRef.current, params.chromePinned.current));
}
