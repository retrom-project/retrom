"use client";

import { useEffect, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import type { EmulatorInstance, ManualScreenshot, PlayerConfig } from "./adapters/ejs-4.2.3-v2";
import { setEmulatorPaused } from "./pause-control";

type Params = {
  emulator: MutableRefObject<EmulatorInstance | undefined>;
  keyboardPauseActionRef: MutableRefObject<() => void>;
  playerMode: MutableRefObject<PlayerConfig["mode"]>;
  running: MutableRefObject<boolean>;
  chromePinned: MutableRefObject<boolean>;
  pausePending: MutableRefObject<boolean>;
  netplayPlayerNo: number | null;
  pausedRef: MutableRefObject<boolean>;
  lastManualScreenshot: MutableRefObject<ManualScreenshot | null>;
  setPaused: Dispatch<SetStateAction<boolean>>;
  setControlsVisible: Dispatch<SetStateAction<boolean>>;
  clearControlsTimer: () => void;
  showControls: () => void;
  showToast: (value: string, timeout?: number) => void;
  toggleNetplayPause: () => unknown;
};

export function usePlayerKeyboardPause(params: Params) {
  const {
    chromePinned, clearControlsTimer, emulator, keyboardPauseActionRef, lastManualScreenshot,
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
      if (!setEmulatorPaused(emulator.current, nextPaused)) {return;}
      pausedRef.current = nextPaused;
      setPaused(nextPaused);
      if (nextPaused) {
        showToast("游戏已暂停，按 P 或点击游戏画面继续");
        setControlsVisible(true);
        clearControlsTimer();
        return;
      }
      lastManualScreenshot.current = null;
      showToast("游戏已继续");
      showControls();
    };
    return () => {keyboardPauseActionRef.current = () => undefined;};
  }, [
    chromePinned, clearControlsTimer, emulator, keyboardPauseActionRef, lastManualScreenshot,
    netplayPlayerNo, pausePending, pausedRef, playerMode, running, setControlsVisible, setPaused,
    showControls, showToast, toggleNetplayPause,
  ]);
}
