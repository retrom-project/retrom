const preferencePrefix = "retrom:preferred-dos-entry:";
const preferenceEvent = "retrom:preferred-dos-entry-change";

export function subscribePreferredDOSEntries(onStoreChange: () => void) {
  window.addEventListener("storage", onStoreChange);
  window.addEventListener(preferenceEvent, onStoreChange);
  return () => {
    window.removeEventListener("storage", onStoreChange);
    window.removeEventListener(preferenceEvent, onStoreChange);
  };
}

export function readPreferredDOSEntry(gameId: string): string | null {
  try {
    const raw = window.localStorage.getItem(`${preferencePrefix}${gameId}`);
    if (raw === null) return null;
    const parsed = JSON.parse(raw) as { version?: unknown; entry?: unknown };
    if (parsed.version !== 1 || parsed.entry !== null && typeof parsed.entry !== "string") return null;
    return raw;
  } catch {
    return null;
  }
}

export function decodePreferredDOSEntry(raw: string | null): { present: boolean; entry: string | null } {
  if (raw === null) return { present: false, entry: null };
  try {
    const parsed = JSON.parse(raw) as { version?: unknown; entry?: unknown };
    if (parsed.version !== 1 || parsed.entry !== null && typeof parsed.entry !== "string") return { present: false, entry: null };
    return { present: true, entry: parsed.entry };
  } catch {
    return { present: false, entry: null };
  }
}

export function writePreferredDOSEntry(gameId: string, entry: string | null) {
  try {
    window.localStorage.setItem(`${preferencePrefix}${gameId}`, JSON.stringify({ version: 1, entry }));
    window.dispatchEvent(new Event(preferenceEvent));
  } catch {
    // Launching must remain available when browser storage is blocked.
  }
}
