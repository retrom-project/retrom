import type { GameRuntimeEvent } from "@xxxsen/retrom-runtime";
import type { Dispatch, SetStateAction } from "react";

import type { PlayerLoadProgress } from "./player-loading";

type OnsRuntimeEventTarget = {
  manualSaveAvailableRef: { current: boolean };
  onExitRequested: () => void;
  setLoadProgress: Dispatch<SetStateAction<PlayerLoadProgress | null>>;
  setManualSaveAvailable: Dispatch<SetStateAction<boolean>>;
  setMessage: Dispatch<SetStateAction<string>>;
  setState: Dispatch<SetStateAction<"loading" | "running" | "error">>;
  setSyncText: Dispatch<SetStateAction<string>>;
  setSyncTone: Dispatch<SetStateAction<"synced" | "busy" | "warning">>;
};

export function handleRetromRuntimeEvent(event: GameRuntimeEvent, target: OnsRuntimeEventTarget) {
  if (event.type === "LOAD_PROGRESS" && event.phase === "PROJECT_CONTENT" && event.totalBytes !== null) {
    target.setLoadProgress({ loadedBytes: event.loadedBytes, totalBytes: event.totalBytes });
  }
  if (event.type === "FATAL_ERROR") {
    target.setState("error");
    target.setMessage(event.code);
  }
  if (event.type === "CHECKPOINT_AVAILABILITY_CHANGED") {
    target.manualSaveAvailableRef.current = event.availability.available;
    target.setManualSaveAvailable(event.availability.available);
    target.setSyncText(event.availability.available ? "可创建存档" : "当前场景暂不可存档");
    target.setSyncTone(event.availability.available ? "synced" : "busy");
  }
  if (event.type === "EXIT_REQUESTED") {
    target.manualSaveAvailableRef.current = false;
    target.setManualSaveAvailable(false);
    target.onExitRequested();
  }
}
