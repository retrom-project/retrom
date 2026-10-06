import { useMemo, useSyncExternalStore } from "react";

const listeners = new Set<() => void>();
let restore: (() => void) | undefined;
let observedSearch = "";
const notify = () => listeners.forEach(listener => listener());
function subscribe(listener: () => void) {
  if (!listeners.size) {
    observedSearch = window.location.search;
    const original = window.history.replaceState;
    const observe = () => {observedSearch = window.location.search; notify();};
    window.history.replaceState = function (data, unused, url) {
      // Next treats its own marked history writes as already synchronized.
      const internal = data?.__NA || data?._N;
      const nextData = data ?? {};
      for (const key of ["__NA", "__PRIVATE_NEXTJS_INTERNALS_TREE"]) {
        if (window.history.state?.[key] !== undefined) {nextData[key] = window.history.state[key];}
      }
      original.call(this, nextData, unused, url);
      if (!internal) {observe();}
    };
    window.addEventListener("popstate", observe);
    restore = () => { window.history.replaceState = original; window.removeEventListener("popstate", observe); };
  }
  listeners.add(listener);
  return () => { listeners.delete(listener); if (!listeners.size) {restore?.();} };
}

export function useMockSearchParams() {
  const search = useSyncExternalStore(subscribe, () => listeners.size ? observedSearch : window.location.search, () => "");
  return useMemo(() => new URLSearchParams(search), [search]);
}
