import { userStorageKey } from "@/features/auth/storage";

const preferenceEvent = "retrom:preferred-core-change";

export function subscribePreferredCores(onStoreChange: () => void) {
  window.addEventListener("storage", onStoreChange);
  window.addEventListener(preferenceEvent, onStoreChange);
  return () => {
    window.removeEventListener("storage", onStoreChange);
    window.removeEventListener(preferenceEvent, onStoreChange);
  };
}

export function readPreferredCore(userId: string | null | undefined, gameId: string): string | null {
  try {
    const key = userStorageKey(userId, "player", `preferred-core:${gameId}`);
    return key ? window.localStorage.getItem(key) : null;
  } catch {
    return null;
  }
}

export function writePreferredCore(userId: string | null | undefined, gameId: string, coreId: string, defaultCoreId: string | undefined) {
  try {
    const key = userStorageKey(userId, "player", `preferred-core:${gameId}`);
    if (!key) return;
    if (!coreId || coreId === defaultCoreId) window.localStorage.removeItem(key);
    else window.localStorage.setItem(key, coreId);
    window.dispatchEvent(new Event(preferenceEvent));
  } catch {
    // Launching must remain available when browser storage is blocked.
  }
}
