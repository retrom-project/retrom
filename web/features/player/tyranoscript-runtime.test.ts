import { describe, expect, it, vi } from "vitest";

import {
  isTyranoScriptLaunchConfig,
  tyranoScriptPlayerInstance,
  validateTyranoScriptLaunchConfig,
} from "./tyranoscript-runtime";

const launchId = "01a06123-1234-7123-8123-123456789abc";
const artifactId = "01a06123-1234-7123-8123-abcdef123456";

function config() {
  const uniqueOrigin = `https://${launchId}.rpg-runtime.example`;
  return {
    runtimeFamily: "TYRANOSCRIPT", protocolVersion: 1, mode: "single", purpose: "PRODUCT",
    launchId, sessionId: launchId, coreId: "tyranoscript", coreName: "TyranoScript",
    gameTitle: "TyranoScript fixture", platformName: "TyranoScript", runtimeVersion: "v0.11.4", artifactId,
    contentDigest: "a".repeat(64), returnTo: "/games/game-1", warnings: [],
    adapter: {
      adapterKind: "TYRANOSCRIPT_WEB", adapterId: "tyranoscript-web",
      bootstrapTicket: "A".repeat(43), uniqueOrigin,
      entryUrl: `${uniqueOrigin}/__retrom/tyranoscript/bootstrap`,
      cleanupUrl: `${uniqueOrigin}/__retrom/tyranoscript/cleanup`,
    },
    checkpoint: null,
  };
}

describe("TyranoScript product runtime", () => {
  it("accepts only the exact product launch shape and isolated endpoints", () => {
    expect(isTyranoScriptLaunchConfig(config())).toBe(true);
    expect(isTyranoScriptLaunchConfig({...config(), purpose: "REVIEW_PREVIEW"})).toBe(false);
    expect(isTyranoScriptLaunchConfig({...config(), extra: true})).toBe(false);
    expect(isTyranoScriptLaunchConfig({...config(), contentDigest: "A".repeat(64)})).toBe(false);
    expect(isTyranoScriptLaunchConfig({
      ...config(), adapter: {...config().adapter, entryUrl: "https://other.example/bootstrap"},
    })).toBe(false);
    expect(() => validateTyranoScriptLaunchConfig({
      ...config(), checkpoint: {payloadKind: "RUNTIME_STATE", payloadUrl: `/runtime/launches/${launchId}/state`},
    })).not.toThrow();
  });

  it("bridges semantic checkpoints into Retrom manual states", async () => {
    const checkpoint = vi.fn(async () => ({
      bytes: new Uint8Array([1, 2, 3]), format: "tyranoscript-snapshot-v1",
    }));
    const runtime = {
      checkpoint, getCanvas: () => null, getCapabilities: () => ({frameCounter: true, volume: true}),
      getCheckpointAvailability: () => ({available: true, blocker: null}), screenshot: vi.fn(),
      getFrameCount: () => 320, pause: vi.fn(), resume: vi.fn(), setVolume: vi.fn(),
    };
    const instance = tyranoScriptPlayerInstance(runtime as never, document.createElement("div"));
    await expect(instance.gameManager?.getStateAsync?.()).resolves.toEqual(new Uint8Array([1, 2, 3]));
    expect(instance.gameManager?.savePayloadKind).toBe("RUNTIME_STATE");
    expect(instance.gameManager?.getFrameNum?.()).toBe(320);
  });
});
