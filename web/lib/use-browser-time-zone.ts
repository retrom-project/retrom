"use client";

import { useSyncExternalStore } from "react";

const hydrationTimeZone = "UTC";
const subscribe = () => () => undefined;

export function useBrowserTimeZone() {
  return useSyncExternalStore(
    subscribe,
    () => Intl.DateTimeFormat().resolvedOptions().timeZone || hydrationTimeZone,
    () => hydrationTimeZone,
  );
}
