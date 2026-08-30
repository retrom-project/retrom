import { describe, expect, it, vi } from "vitest";

import {
  butterscotchPlayerInstance,
  isButterscotchLaunchConfig,
  validateButterscotchLaunchConfig,
} from "./butterscotch-runtime";

const launchId = "01a05123-1234-7123-8123-123456789abc";
const artifactId = "01a05123-1234-7123-8123-abcdef123456";
function config() {
  return {
    runtimeFamily: "BUTTERSCOTCH", protocolVersion: 1, mode: "single", purpose: "PRODUCT",
    launchId, sessionId: launchId, coreId: "butterscotch", coreName: "Butterscotch",
    gameTitle: "GameMaker fixture", platformName: "GameMaker", runtimeVersion: "v0.8.0", artifactId,
    contentDigest: "a".repeat(64), returnTo: "/games/game-1", warnings: [],
    adapter: {
      adapterKind: "BUTTERSCOTCH_WEB", adapterId: "butterscotch-web",
      runtimeBaseUrl: "/runtime/retrom-runtime/v0.8.0/",
      projectIndexUrl: `/runtime/content/project/${"b".repeat(64)}/index.json`,
    }, checkpoint: null,
  };
}

describe("Butterscotch product runtime", () => {
  it("accepts only the exact product launch shape", () => {
    expect(isButterscotchLaunchConfig(config())).toBe(true);
    expect(isButterscotchLaunchConfig({ ...config(), purpose: "REVIEW_PREVIEW" })).toBe(false);
    expect(isButterscotchLaunchConfig({ ...config(), extra: true })).toBe(false);
    expect(isButterscotchLaunchConfig({ ...config(), contentDigest: "A".repeat(64) })).toBe(false);
    expect(() => validateButterscotchLaunchConfig({
      ...config(), checkpoint: { payloadKind: "RUNTIME_STATE", payloadUrl: `/runtime/launches/${launchId}/state` },
    })).not.toThrow();
  });

  it("bridges runtime checkpoints into Retrom manual states", async () => {
    const checkpoint = vi.fn(async () => ({bytes: new Uint8Array([1, 2, 3]), format: "butterscotch-checkpoint-v1"}));
    const runtime = {
      checkpoint, getCanvas: () => null, getCapabilities: () => ({frameCounter: false, volume: false}),
      getCheckpointAvailability: () => ({available: true, blocker: null}), screenshot: vi.fn(),
    };
    const instance = butterscotchPlayerInstance(runtime as never, document.createElement("div"));
    await expect(instance.gameManager?.getStateAsync?.()).resolves.toEqual(new Uint8Array([1, 2, 3]));
    expect(instance.gameManager?.savePayloadKind).toBe("RUNTIME_STATE");
  });
});
