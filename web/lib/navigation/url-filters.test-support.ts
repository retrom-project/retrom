import { useMemo, useSyncExternalStore } from "react";

const listeners = new Set<() => void>();
let restore: (() => void) | undefined;
const notify = () => listeners.forEach(listener => listener());
function subscribe(listener: () => void) {
  if (!listeners.size) {
    const original = window.history.replaceState;
    window.history.replaceState = function (...args) { original.apply(this, args); notify(); };
    window.addEventListener("popstate", notify);
    restore = () => { window.history.replaceState = original; window.removeEventListener("popstate", notify); };
  }
  listeners.add(listener);
  return () => { listeners.delete(listener); if (!listeners.size) {restore?.();} };
}

export function useMockSearchParams() {
  const search = useSyncExternalStore(subscribe, () => window.location.search, () => "");
  return useMemo(() => new URLSearchParams(search), [search]);
}
