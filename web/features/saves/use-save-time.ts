"use client";

import { useSyncExternalStore } from "react";
import { formatSaveTime } from "./save-library";

const subscribe = () => () => undefined;
const clientSnapshot = () => true;
const serverSnapshot = () => false;

export function useSaveTimeFormatter() {
  const browserTimeReady = useSyncExternalStore(subscribe, clientSnapshot, serverSnapshot);
  return (value: number, nowMs: number, includeSeconds = true) =>
    formatSaveTime(value, nowMs, includeSeconds, browserTimeReady ? "local" : "utc");
}
