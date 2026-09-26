import {userStorageKey} from "@/features/auth/storage";

const launchContextKey = "retrom:player-game";

/** UI preference context only; runtime authorization always comes from the Launch. */
export function rememberPlayerGame(launchId: string, gameId: string) {
  try {window.sessionStorage.setItem(launchContextKey, JSON.stringify({launchId, gameId}));}
  catch { /* Storage is optional for play. */ }
}

export function readPlayerGame(launchId: string): string | null {
  try {
    const value: unknown = JSON.parse(window.sessionStorage.getItem(launchContextKey) ?? "null");
    return value !== null && typeof value === "object" && "launchId" in value && value.launchId === launchId &&
      "gameId" in value && typeof value.gameId === "string" ? value.gameId : null;
  } catch {return null;}
}

export function readGamepadCursorPreference(userId: string | undefined, gameId: string | null): boolean | null {
  try {
    const key = gameId && userStorageKey(userId, "player", `gamepad-cursor:${gameId}`);
    const value = key ? window.localStorage.getItem(key) : null;
    return value === "true" ? true : value === "false" ? false : null;
  } catch {return null;}
}

export function writeGamepadCursorPreference(userId: string | undefined, gameId: string | null, enabled: boolean) {
  try {
    const key = gameId && userStorageKey(userId, "player", `gamepad-cursor:${gameId}`);
    if (key) {window.localStorage.setItem(key, String(enabled));}
  } catch { /* Keep the current session usable when storage is unavailable. */ }
}
