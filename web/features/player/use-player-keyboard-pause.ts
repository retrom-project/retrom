"use client";

import { useEffect, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import type {PlayerRuntimeV1} from "./runtime/contract";

type Params = {
  runtime: MutableRefObject<PlayerRuntimeV1 | null>;
  keyboardPauseActionRef: MutableRefObject<() => void>;
  playerMode: MutableRefObject<"single" | "netplay">;
  running: MutableRefObject<boolean>;
  chromePinned: MutableRefObject<boolean>;
  pausePending: MutableRefObject<boolean>;
  netplayPlayerNo: number | null;
  pausedRef: MutableRefObject<boolean>;
  setPaused: Dispatch<SetStateAction<boolean>>;
  setControlsVisible: Dispatch<SetStateAction<boolean>>;
  clearControlsTimer: () => void;
  showControls: () => void;
  showToast: (value: string, timeout?: number) => void;
  toggleNetplayPause: () => unknown;
};

export function usePlayerKeyboardPause(params: Params) {
  const {
    chromePinned, clearControlsTimer, runtime, keyboardPauseActionRef,
    netplayPlayerNo, pausePending, pausedRef, playerMode, running, setControlsVisible,
    setPaused, showControls, showToast, toggleNetplayPause,
  } = params;
  useEffect(() => {
    keyboardPauseActionRef.current = () => {
      if (!running.current || chromePinned.current || pausePending.current) {return;}
      if (playerMode.current === "netplay") {
        if (netplayPlayerNo !== 1) {showToast("只有 P1 可以暂停联机", 3_000); return;}
        void toggleNetplayPause();
        return;
      }
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
    netplayPlayerNo, pausePending, pausedRef, playerMode, running, setControlsVisible, setPaused,
    showControls, showToast, toggleNetplayPause,
  ]);
}
