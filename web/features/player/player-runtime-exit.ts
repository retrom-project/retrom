"use client";

import { useCallback, type Dispatch, type SetStateAction } from "react";

type Mutable<T> = { current: T };
type SyncTone = "synced" | "busy" | "warning";

export function useRuntimeExitHandler(
  manualSaveAvailableRef: Mutable<boolean>,
  setManualSaveAvailable: Dispatch<SetStateAction<boolean>>,
  setSyncText: Dispatch<SetStateAction<string>>,
  setSyncTone: Dispatch<SetStateAction<SyncTone>>,
  experience: "standard" | "immersive",
  exit: () => Promise<void>,
  exitImmersive: () => Promise<void>,
) {
  return useCallback(() => {
    manualSaveAvailableRef.current = false;
    setManualSaveAvailable(false);
    setSyncText("游戏已退出");
    setSyncTone("warning");
    void (experience === "immersive" ? exitImmersive() : exit());
  }, [exit, exitImmersive, experience, manualSaveAvailableRef, setManualSaveAvailable, setSyncText, setSyncTone]);
}
