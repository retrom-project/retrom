import {userStorageKey} from "@/features/auth/storage";

import type {RuntimeContentLoadingV1} from "./runtime/contract";

export type ContentLoadingCapability = RuntimeContentLoadingV1;
export type ContentLoading = "ON_DEMAND" | "PRELOAD";
const changed = "retrom:content-loading-change";

export function readContentLoading(userId: string | null | undefined): ContentLoading {
  try {
    const key = userStorageKey(userId, "player", "content-loading");
    return key && window.localStorage.getItem(key) === "PRELOAD" ? "PRELOAD" : "ON_DEMAND";
  } catch {return "ON_DEMAND";}
}

export function writeContentLoading(userId: string | null | undefined, value: ContentLoading) {
  const key = userStorageKey(userId, "player", "content-loading");
  if (!key) {return;}
  try {
    window.localStorage.setItem(key, value);
    window.dispatchEvent(new Event(changed));
  } catch { /* The default streaming mode remains available when device preferences cannot be stored. */ }
}

export function subscribeContentLoading(listener: () => void) {
  window.addEventListener("storage", listener);
  window.addEventListener(changed, listener);
  return () => {
    window.removeEventListener("storage", listener);
    window.removeEventListener(changed, listener);
  };
}

/** Resolve against the actual Launch Target, including restores and quick launches. */
export function resolveContentLoading(capability: ContentLoadingCapability | null | undefined,
  preference: ContentLoading, purpose: "PRODUCT" | "REVIEW_PREVIEW"): ContentLoading | undefined {
  if (purpose === "REVIEW_PREVIEW") {return "ON_DEMAND";}
  if (capability === "PRELOAD_ONLY") {return "PRELOAD";}
  return capability === "ON_DEMAND_AND_PRELOAD" ? preference : undefined;
}
