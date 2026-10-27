"use client";

import {useCallback, useRef, useSyncExternalStore, type RefObject} from "react";
import type {PlayerRuntimeV1, RuntimeStateV1} from "./runtime/contract";
import {writeTouchGamepadPreference} from "./touch-gamepad-preference";

const changeEvent = "retrom:touch-gamepad-change";

export function usePlayerTouchGamepad(runtime: RefObject<PlayerRuntimeV1 | null>, state: RuntimeStateV1,
  coreId: string, onError: (message: string) => void) {
  const pending = useRef(false);
  const subscribe = useCallback((onChange: () => void) => {
    window.addEventListener(changeEvent, onChange);
    const unsubscribe = state === "CREATED" ? undefined : runtime.current?.subscribe(onChange);
    return () => {unsubscribe?.(); window.removeEventListener(changeEvent, onChange);};
  }, [runtime, state]);
  const snapshot = useCallback(() => {
    if (state !== "RUNNING" && state !== "PAUSED" && state !== "CHECKPOINTING") {return 0;}
    const controls = runtime.current?.getTouchGamepad?.();
    return controls ? controls.getState().enabled ? 1 : 2 : 0;
  }, [runtime, state]);
  const value = useSyncExternalStore(subscribe, snapshot, () => 0);
  async function toggle() {
    const controls = runtime.current?.getTouchGamepad?.();
    if (!controls || pending.current) {return;}
    pending.current = true;
    try {
      const enabled = !controls.getState().enabled;
      await controls.setEnabled(enabled);
      writeTouchGamepadPreference(coreId, enabled);
      window.dispatchEvent(new Event(changeEvent));
    } catch {onError("无法切换虚拟手柄，请重试。");}
    finally {pending.current = false;}
  }
  return value ? {enabled: value === 1, toggle} : null;
}
