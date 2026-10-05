"use client";

import { useCallback, useEffect, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import type {PlayerRuntimeV2} from "./runtime/contract";

type Params = {
  runtime: MutableRefObject<PlayerRuntimeV2 | null>;
  keyboardPauseActionRef: MutableRefObject<() => void>;
  running: MutableRefObject<boolean>;
  chromePinned: MutableRefObject<boolean>;
  pausePending: MutableRefObject<boolean>;
  pausedRef: MutableRefObject<boolean>;
  setPaused: Dispatch<SetStateAction<boolean>>;
  setControlsVisible: Dispatch<SetStateAction<boolean>>;
  clearControlsTimer: () => void;
  showControls: () => void;
  showToast: (value: string, timeout?: number) => void;
  };

export function usePlayerKeyboardPause(params: Params) {
  const {
    chromePinned, clearControlsTimer, runtime, keyboardPauseActionRef,
    pausePending, pausedRef, running, setControlsVisible,
    setPaused, showControls, showToast,
  } = params;
  useEffect(() => {
    keyboardPauseActionRef.current = () => {
      if (!running.current || chromePinned.current || pausePending.current) {return;}
      const nextPaused = !pausedRef.current;
      const active = runtime.current;
      if (!active?.getCapabilities().pause) {return;}
      void (nextPaused ? active.pause() : active.resume()).then(() => {
        pausedRef.current = nextPaused;
        setPaused(nextPaused);
        if (nextPaused) {
          showToast("游戏已暂停，按 P 或点击游戏画面继续");
          setControlsVisible(true);
          clearControlsTimer();
          return;
        }
        showToast("游戏已继续");
        showControls();
      }).catch(() => showToast("无法更改暂停状态", 3_000));
    };
    return () => {keyboardPauseActionRef.current = () => undefined;};
  }, [
    chromePinned, clearControlsTimer, runtime, keyboardPauseActionRef,
    pausePending, pausedRef, running, setControlsVisible, setPaused,
    showControls, showToast,
  ]);
}


export function usePauseForToolbar({runtime: runtimeRef, running: runningRef, pausePending: pendingRef, pausedRef,
  setPaused, setControlsVisible, clearControlsTimer, showToast,
}: Pick<Params, "runtime" | "running" | "pausePending" | "pausedRef" | "setPaused" | "setControlsVisible" | "clearControlsTimer" | "showToast">) {
  return useCallback(() => {
    if (!runningRef.current || pausedRef.current || pendingRef.current) {return;}
    const active = runtimeRef.current;
    if (!active?.getCapabilities().pause) {return;}
    pendingRef.current = true;
    void active.pause().then(() => {
      if (!runningRef.current) {return;}
      pausedRef.current = true;
      setPaused(true);
      showToast("游戏已暂停，点击游戏画面继续");
      setControlsVisible(true);
      clearControlsTimer();
    }).catch(() => showToast("无法暂停游戏", 3_000)).finally(() => {pendingRef.current = false;});
  }, [runtimeRef, runningRef, pendingRef, pausedRef, setPaused, setControlsVisible, clearControlsTimer, showToast]);
}
