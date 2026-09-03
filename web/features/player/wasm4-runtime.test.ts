import { describe, expect, it, vi } from "vitest";

import {
  isWASM4LaunchConfig,
  validateWASM4LaunchConfig,
  wasm4PlayerInstance,
} from "./wasm4-runtime";

const launchId = "01a05123-1234-7123-8123-123456789abc";
const artifactId = "01a05123-1234-7123-8123-abcdef123456";

function config() {
  return {
    runtimeFamily: "WASM4", protocolVersion: 1, mode: "single", purpose: "PRODUCT",
    launchId, sessionId: launchId, coreId: "wasm4", coreName: "WASM-4",
    gameTitle: "Pong", platformName: "WASM-4", runtimeVersion: "v0.11.4", artifactId,
    contentDigest: "a".repeat(64), cartSizeBytes: 6818, returnTo: "/games/game-1", warnings: [],
    adapter: {
      adapterKind: "WASM4_WEB", adapterId: "wasm4-web",
      cartUrl: `/runtime/content/game/${"b".repeat(64)}/pong.wasm`,
      runtimeBaseUrl: "/runtime/retrom-runtime/v0.11.4/",
    }, checkpoint: null,
  };
}

describe("WASM-4 product runtime", () => {
  it("accepts only the exact bounded product launch shape", () => {
    expect(isWASM4LaunchConfig(config())).toBe(true);
    expect(isWASM4LaunchConfig({...config(), purpose: "REVIEW_PREVIEW"})).toBe(false);
    expect(isWASM4LaunchConfig({...config(), cartSizeBytes: 65537})).toBe(false);
    expect(isWASM4LaunchConfig({...config(), extra: true})).toBe(false);
    expect(() => validateWASM4LaunchConfig({
      ...config(), checkpoint: {
        payloadKind: "RUNTIME_STATE", payloadUrl: `/runtime/launches/${launchId}/state`,
      },
    })).not.toThrow();
  });

  it("bridges wasm4-state-v1 checkpoints into Retrom manual states", async () => {
    const checkpoint = vi.fn(async () => ({bytes: new Uint8Array([1, 2, 3]), format: "wasm4-state-v1"}));
    const runtime = {
      checkpoint, getCanvas: () => null, getCapabilities: () => ({frameCounter: true, volume: false}),
      getCheckpointAvailability: () => ({available: true, blocker: null}), screenshot: vi.fn(),
      getFrameCount: () => 42,
    };
    const instance = wasm4PlayerInstance(runtime as never, document.createElement("div"));
    await expect(instance.gameManager?.getStateAsync?.()).resolves.toEqual(new Uint8Array([1, 2, 3]));
    expect(instance.gameManager?.savePayloadKind).toBe("RUNTIME_STATE");
    expect(instance.gameManager?.getFrameNum?.()).toBe(42);
  });
});
