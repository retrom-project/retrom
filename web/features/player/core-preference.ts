const preferencePrefix = "retrom:preferred-core:";
const preferenceEvent = "retrom:preferred-core-change";

export function subscribePreferredCores(onStoreChange: () => void) {
  window.addEventListener("storage", onStoreChange);
  window.addEventListener(preferenceEvent, onStoreChange);
  return () => {
    window.removeEventListener("storage", onStoreChange);
    window.removeEventListener(preferenceEvent, onStoreChange);
  };
}

export function readPreferredCore(gameId: string): string | null {
  try {
    return window.localStorage.getItem(`${preferencePrefix}${gameId}`);
  } catch {
    return null;
  }
}

export function writePreferredCore(gameId: string, coreId: string, defaultCoreId: string | undefined) {
  try {
    const key = `${preferencePrefix}${gameId}`;
    if (!coreId || coreId === defaultCoreId) window.localStorage.removeItem(key);
    else window.localStorage.setItem(key, coreId);
    window.dispatchEvent(new Event(preferenceEvent));
  } catch {
    // Launching must remain available when browser storage is blocked.
  }
}
