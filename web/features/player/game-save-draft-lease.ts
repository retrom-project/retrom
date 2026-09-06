import {newUuid} from "@/lib/crypto";

function key(userId: string, launchId: string) {return `retrom:local-game-save:${userId}:${launchId}`;}
export function draftIsActive(userId: string, launchId: string) {
  try {
    const value: {expires?: number} = JSON.parse(localStorage.getItem(key(userId, launchId)) ?? "{}");
    return typeof value.expires === "number" && value.expires > Date.now();
  } catch {return false;}
}

export function holdDraftLease(userId: string, launchId: string) {
  const token = newUuid(), storageKey = key(userId, launchId);
  const refresh = () => {
    try {localStorage.setItem(storageKey, JSON.stringify({token, expires: Date.now() + 30_000}));} catch { /* Draft storage reports failures separately. */ }
  };
  refresh();
  const timer = window.setInterval(refresh, 10_000);
  const release = () => {
    window.clearInterval(timer);
    try {
      const value: {token?: string} = JSON.parse(localStorage.getItem(storageKey) ?? "{}");
      if (value.token === token) {localStorage.removeItem(storageKey);}
    } catch { /* Expiration handles an interrupted lease. */ }
    window.removeEventListener("pagehide", release);
  };
  window.addEventListener("pagehide", release);
  return release;
}
