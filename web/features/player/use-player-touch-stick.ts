"use client";

import {useCallback, useState, useSyncExternalStore, type RefObject} from "react";
import type {PlayerRuntimeV1, RuntimeStateV1, RuntimeTouchStickModeV1} from "./runtime/contract";
import {readTouchStickPreference, writeTouchStickPreference} from "./touch-stick-preference";

export type PlayerTouchStick = {
  /** The saved choice, which the next launch of this core uses. */
  mode: RuntimeTouchStickModeV1;
  /** True while the saved choice differs from the mode this launch started with. */
  pending: boolean;
  /** Saves the other mode and reports whether it now differs from the running game, i.e. needs a restart. */
  toggle: () => boolean;
};

/** Stick mode is read when a game launches, so switching only records the choice for the next launch. */
export function usePlayerTouchStick(runtime: RefObject<PlayerRuntimeV1 | null>, state: RuntimeStateV1,
  coreId: string): PlayerTouchStick | null {
  const [saved, setSaved] = useState(() => readTouchStickPreference(coreId));
  const subscribe = useCallback((onChange: () => void) =>
    state === "CREATED" ? () => undefined : runtime.current?.subscribe(onChange) ?? (() => undefined), [runtime, state]);
  const snapshot = useCallback(() => {
    if (state !== "RUNNING" && state !== "PAUSED" && state !== "CHECKPOINTING") {return null;}
    return runtime.current?.getTouchStick?.()?.getState().mode ?? null;
  }, [runtime, state]);
  const current = useSyncExternalStore(subscribe, snapshot, () => null);
  if (!current) {return null;}
  return {
    mode: saved,
    pending: saved !== current,
    toggle: () => {
      const next = saved === "FLOATING" ? "FIXED" : "FLOATING";
      writeTouchStickPreference(coreId, next);
      setSaved(next);
      return next !== current;
    },
  };
}
