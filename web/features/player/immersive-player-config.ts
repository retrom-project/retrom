import type { PlayerConfig } from "./adapters/ejs-4.2.3-v2";

export function validateImmersivePlayerConfig(config: PlayerConfig) {
  if (config.mode !== "single") {throw new Error("PLAYER_IMMERSIVE_SINGLE_ONLY");}
  const base = new URL("https://retrom.invalid");
  const returnURL = new URL(config.returnTo, base);
  if (returnURL.origin !== base.origin || returnURL.hash || !validImmersiveReturn(returnURL, config.stateUrl !== null)) {
    throw new Error("PLAYER_IMMERSIVE_RETURN_INVALID");
  }
}

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

function validImmersiveReturn(url: URL, restoringSave: boolean) {
  if (restoringSave) {return validSaveLibraryReturn(url);}
  if (/^\/immersive\/platforms\/[^/]+$/.test(url.pathname)) {
    return exactQuery(url, ["gameId"]) && uuid.test(url.searchParams.get("gameId") ?? "");
  }
  const library = /^\/immersive\/library\/(all|recent|favorites)$/.exec(url.pathname);
  if (!library) {return false;}
  const allowed = library[1] === "favorites" ? ["gameId", "folderId"] : ["gameId"];
  return optionalUUIDQuery(url, allowed);
}

function validSaveLibraryReturn(url: URL) {
  return url.pathname === "/immersive/library/saves" && exactQuery(url, ["gameId", "saveStateId"]) &&
    uuid.test(url.searchParams.get("gameId") ?? "") && uuid.test(url.searchParams.get("saveStateId") ?? "");
}

function exactQuery(url: URL, keys: string[]) {
  const actual = [...url.searchParams.keys()];
  return actual.length === keys.length && new Set(actual).size === actual.length &&
    keys.every((key) => url.searchParams.has(key));
}

function optionalUUIDQuery(url: URL, allowed: string[]) {
  const actual = [...url.searchParams.keys()];
  return url.searchParams.has("gameId") && new Set(actual).size === actual.length &&
    actual.every((key) => allowed.includes(key)) &&
    actual.every((key) => uuid.test(url.searchParams.get(key) ?? ""));
}
