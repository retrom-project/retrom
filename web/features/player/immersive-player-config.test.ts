import { describe, expect, it } from "vitest";
import type { PlayerConfig } from "./adapters/ejs-4.2.3-v2";
import { validateImmersivePlayerConfig } from "./immersive-player-config";

const config = {
  mode: "single",
  stateUrl: null,
  returnTo: "/immersive/platforms/arcade?gameId=01980000-0000-7000-8000-000000000001",
} as PlayerConfig;

describe("validateImmersivePlayerConfig", () => {
  it("accepts single-player platform and library return routes", () => {
    expect(() => validateImmersivePlayerConfig(config)).not.toThrow();
    for (const returnTo of [
      "/immersive/library/all?gameId=01980000-0000-7000-8000-000000000001",
      "/immersive/library/recent?gameId=01980000-0000-7000-8000-000000000001",
      "/immersive/library/favorites?gameId=01980000-0000-7000-8000-000000000001&folderId=01980000-0000-7000-8000-000000000002",
    ]) {
      expect(() => validateImmersivePlayerConfig({ ...config, returnTo })).not.toThrow();
    }
    expect(() => validateImmersivePlayerConfig({ ...config, mode: "netplay" })).toThrow("PLAYER_IMMERSIVE_SINGLE_ONLY");
  });

  it("allows a save restore only when it returns to the exact immersive save record", () => {
    const restored = {
      ...config,
      stateUrl: "/runtime/launches/id/state",
      returnTo: "/immersive/library/saves?gameId=01980000-0000-7000-8000-000000000001&saveStateId=01980000-0000-7000-8000-000000000002",
    };
    expect(() => validateImmersivePlayerConfig(restored)).not.toThrow();
    expect(() => validateImmersivePlayerConfig({ ...restored, returnTo: config.returnTo })).toThrow("PLAYER_IMMERSIVE_RETURN_INVALID");
    expect(() => validateImmersivePlayerConfig({ ...restored, mode: "netplay" })).toThrow("PLAYER_IMMERSIVE_SINGLE_ONLY");
    expect(() => validateImmersivePlayerConfig({ ...restored, returnTo: `${restored.returnTo}&extra=value` })).toThrow("PLAYER_IMMERSIVE_RETURN_INVALID");
  });

  it.each([
    "/library",
    "/immersive/platforms/arcade",
    "/immersive/platforms/arcade?gameId=game&extra=value",
    "/immersive/library/all",
    "/immersive/library/saves?gameId=01980000-0000-7000-8000-000000000001&saveStateId=01980000-0000-7000-8000-000000000002",
    "/immersive/library/recent?folderId=01980000-0000-7000-8000-000000000002",
    "/immersive/library/favorites?gameId=01980000-0000-7000-8000-000000000001&extra=value",
    "https://example.test/immersive/platforms/arcade?gameId=game",
  ])("rejects returnTo outside the exact immersive game list shape: %s", (returnTo) => {
    expect(() => validateImmersivePlayerConfig({ ...config, returnTo })).toThrow("PLAYER_IMMERSIVE_RETURN_INVALID");
  });
});
