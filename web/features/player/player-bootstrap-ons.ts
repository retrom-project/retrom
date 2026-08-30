import type { GameRuntimeEvent } from "@xxxsen/retrom-runtime";
import type { Dispatch, SetStateAction } from "react";

import type { PlayerLoadProgress } from "./player-loading";

type OnsRuntimeEventTarget = {
  setLoadProgress: Dispatch<SetStateAction<PlayerLoadProgress | null>>;
  setMessage: Dispatch<SetStateAction<string>>;
  setState: Dispatch<SetStateAction<"loading" | "running" | "error">>;
};

export function handleOnsRuntimeEvent(event: GameRuntimeEvent, target: OnsRuntimeEventTarget) {
  if (event.type === "LOAD_PROGRESS" && event.phase === "PROJECT_CONTENT" && event.totalBytes !== null) {
    target.setLoadProgress({ loadedBytes: event.loadedBytes, totalBytes: event.totalBytes });
  }
  if (event.type === "FATAL_ERROR") {
    target.setState("error");
    target.setMessage(event.code);
  }
}
