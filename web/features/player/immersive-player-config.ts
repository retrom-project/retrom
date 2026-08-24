import type { PlayerConfig } from "./adapters/ejs-4.2.3-v2";

export function validateImmersivePlayerConfig(config: PlayerConfig) {
  if (config.mode !== "single" || config.stateUrl !== null) {
    throw new Error("PLAYER_IMMERSIVE_SINGLE_ONLY");
  }
  const base = new URL("https://retrom.invalid");
  const returnURL = new URL(config.returnTo, base);
  const platformList = /^\/immersive\/platforms\/[^/]+$/.test(returnURL.pathname);
  const gameId = returnURL.searchParams.get("gameId");
  if (returnURL.origin !== base.origin || !platformList || !gameId || returnURL.searchParams.size !== 1 || returnURL.hash) {
    throw new Error("PLAYER_IMMERSIVE_RETURN_INVALID");
  }
}
