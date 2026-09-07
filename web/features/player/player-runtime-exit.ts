"use client";

import { useCallback, type Dispatch, type SetStateAction } from "react";

import type {RuntimeFinalSnapshotV1} from "./runtime/contract";

type Mutable<T> = { current: T };
type SyncTone = "synced" | "busy" | "warning";

export function useRuntimeExitHandler(
  manualSaveAvailableRef: Mutable<boolean>,
  setManualSaveAvailable: Dispatch<SetStateAction<boolean>>,
  setSyncText: Dispatch<SetStateAction<string>>,
  setSyncTone: Dispatch<SetStateAction<SyncTone>>,
  experience: "standard" | "immersive",
  exit: (snapshot?: RuntimeFinalSnapshotV1) => Promise<void>,
  exitImmersive: (snapshot?: RuntimeFinalSnapshotV1) => Promise<void>,
) {
  return useCallback((snapshot?: RuntimeFinalSnapshotV1) => {
    manualSaveAvailableRef.current = false;
    setManualSaveAvailable(false);
    setSyncText("游戏已退出");
    setSyncTone("warning");
    void (experience === "immersive" ? exitImmersive(snapshot) : exit(snapshot));
  }, [exit, exitImmersive, experience, manualSaveAvailableRef, setManualSaveAvailable, setSyncText, setSyncTone]);
}
