"use client";

import { useSyncExternalStore } from "react";

function subscribe(listener: () => void) {
  document.addEventListener("fullscreenchange", listener);
  return () => document.removeEventListener("fullscreenchange", listener);
}

export function useOverlayTarget() {
  return useSyncExternalStore(
    subscribe,
    () => document.fullscreenElement instanceof HTMLElement ? document.fullscreenElement : document.body,
    () => null,
  );
}
