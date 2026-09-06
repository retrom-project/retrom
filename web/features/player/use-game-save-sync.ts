"use client";

import {useEffect, useRef} from "react";
import {GameSaveSync, type GameSavePresentation} from "./game-save-sync";
import type {PlayerRuntimeV1} from "./runtime/contract";
import type {RuntimeSavePayload} from "./runtime/runtime-actions";

export function useGameSaveSync(
  enabled: boolean,
  runtime: {current: PlayerRuntimeV1 | null},
  upload: (payload: RuntimeSavePayload) => Promise<boolean>,
  present: (state: GameSavePresentation) => void,
) {
  const sync = useRef<GameSaveSync | null>(null);
  useEffect(() => {
    if (!enabled || !runtime.current) {return;}
    const active = new GameSaveSync(runtime.current, upload, present);
    sync.current = active;
    active.start();
    return () => {
      void active.stop();
      if (sync.current === active) {sync.current = null;}
    };
  }, [enabled, runtime, upload, present]);
  return sync;
}
