"use client";
import {useEffect, useEffectEvent, useLayoutEffect, useRef, useSyncExternalStore, type RefObject} from "react";
import type {PlayerRuntimeV1, RuntimeStateV1} from "./runtime/contract";
import {OrientationPauseGate, phoneLandscape} from "./player-orientation";

function subscribe(listener: () => void) {
  window.addEventListener("resize", listener);
  window.addEventListener("orientationchange", listener);
  window.screen.orientation?.addEventListener("change", listener);
  document.addEventListener("visibilitychange", listener);
  return () => {
    window.removeEventListener("resize", listener);
    window.removeEventListener("orientationchange", listener);
    window.screen.orientation?.removeEventListener("change", listener);
    document.removeEventListener("visibilitychange", listener);
  };
}
export function usePlayerOrientation() {
  const status = useSyncExternalStore(subscribe, () => {
    const landscape = phoneLandscape(window);
    return landscape === null ? "desktop" : landscape ? "landscape" : "portrait";
  }, () => "unknown");
  return {blocked: status === "unknown" || status === "portrait", known: status !== "unknown"};
}
export function useOrientationPause(runtime: RefObject<PlayerRuntimeV1 | null>, state: RuntimeStateV1,
  blocked: boolean, otherOverlay: boolean, onError: (message: string) => void) {
  const gate = useRef<OrientationPauseGate | null>(null);
  const report = useEffectEvent(onError);
  const visible = useSyncExternalStore(subscribe, () => document.visibilityState !== "hidden", () => false);
  const ready = ["RUNNING", "PAUSED", "CHECKPOINTING"].includes(state);
  useLayoutEffect(() => {
    const instance = runtime.current;
    if (!instance || !ready) { return; }
    const current = new OrientationPauseGate(instance, message => report(message));
    gate.current = current;
    return () => { current.stop(); gate.current = null; };
  }, [runtime, ready]);
  useLayoutEffect(() => { void gate.current?.update(blocked, !otherOverlay && visible); }, [blocked, otherOverlay, visible, state]);
  useEffect(() => {
    const instance = runtime.current;
    if (!instance) { return; }
    return instance.subscribe(event => {
      if (event.type === "STATE_CHANGED") { void gate.current?.update(blocked, !otherOverlay && document.visibilityState !== "hidden"); }
    });
  }, [runtime, state, blocked, otherOverlay]);
  return {manualPause: () => gate.current?.manualPause()};
}
