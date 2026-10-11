"use client";

import {useState, useSyncExternalStore} from "react";
const mobilePlayerQuery = "(max-width: 1279px), (hover: none) and (pointer: coarse)";
const desktopPointerQuery = "(hover: hover) and (pointer: fine)";

function subscribe(onChange: () => void, mediaQuery = mobilePlayerQuery) {
  if (typeof window.matchMedia !== "function") {return () => {};}
  const query = window.matchMedia(mediaQuery);
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
}

function subscribeDesktopPointer(onChange: () => void) { return subscribe(onChange, desktopPointerQuery); }
export function useDesktopPlayerPointer() {
  return useSyncExternalStore(subscribeDesktopPointer,
    () => typeof window.matchMedia === "function" && window.matchMedia(desktopPointerQuery).matches,
    () => false);
}

export function useMobilePlayerLayout() {
  return useSyncExternalStore(subscribe,
    () => typeof window.matchMedia === "function" && window.matchMedia(mobilePlayerQuery).matches,
    () => false);
}

export function usePlayerDebugState() {
  const mobile = useMobilePlayerLayout();
  const [requested, setRequested] = useState(false);
  return [!mobile && requested, setRequested] as const;
}
