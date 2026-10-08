"use client";

import {useState, useSyncExternalStore} from "react";
const mobilePlayerQuery = "(max-width: 1279px), (hover: none) and (pointer: coarse)";

function subscribe(onChange: () => void) {
  if (typeof window.matchMedia !== "function") {return () => {};}
  const query = window.matchMedia(mobilePlayerQuery);
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
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
