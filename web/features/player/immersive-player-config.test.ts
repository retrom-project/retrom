import { describe, expect, it } from "vitest";
import type { PlayerConfig } from "./adapters/ejs-4.2.3-v2";
import { validateImmersivePlayerConfig } from "./immersive-player-config";

const config = {
  mode: "single",
  stateUrl: null,
  returnTo: "/immersive/platforms/arcade?gameId=01980000-0000-7000-8000-000000000001",
} as PlayerConfig;

describe("validateImmersivePlayerConfig", () => {
  it("accepts only a fresh single-player launch returning to its immersive game", () => {
    expect(() => validateImmersivePlayerConfig(config)).not.toThrow();
    expect(() => validateImmersivePlayerConfig({ ...config, mode: "netplay" })).toThrow("PLAYER_IMMERSIVE_SINGLE_ONLY");
    expect(() => validateImmersivePlayerConfig({ ...config, stateUrl: "/runtime/launches/id/state" })).toThrow("PLAYER_IMMERSIVE_SINGLE_ONLY");
  });

  it.each([
    "/library",
    "/immersive/platforms/arcade",
    "/immersive/platforms/arcade?gameId=game&extra=value",
    "https://example.test/immersive/platforms/arcade?gameId=game",
  ])("rejects returnTo outside the exact immersive game list shape: %s", (returnTo) => {
    expect(() => validateImmersivePlayerConfig({ ...config, returnTo })).toThrow("PLAYER_IMMERSIVE_RETURN_INVALID");
  });
});
