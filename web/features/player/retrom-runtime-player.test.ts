import type { GameRuntime, RuntimeCapabilities } from "@xxxsen/retrom-runtime";
import { describe, expect, it, vi } from "vitest";

import { retromRuntimePlayerInstance } from "./retrom-runtime-player";

describe("retrom-runtime Player bridge", () => {
  it("maps the engine-neutral checkpoint and blocker contracts into the Player shell", async () => {
    const runtime = runtimeFixture({
      checkpoint: vi.fn(async () => ({bytes: new Uint8Array([4, 2]), format: "ons-save-bundle-v1"})),
      getCheckpointAvailability: () => ({available: false, blocker: "NOT_READY"}),
    });
    const target = document.createElement("div");
    const instance = retromRuntimePlayerInstance(runtime, target, {
      checkpointFormat: "ons-save-bundle-v1", payloadKind: "ONS_SAVE_BUNDLE_V1",
    });

    expect(await instance.gameManager?.getStateAsync?.()).toEqual(new Uint8Array([4, 2]));
    expect(instance.gameManager?.getCheckpointAvailability?.()).toEqual({
      available: false, reason: "RUNTIME_NOT_READY",
    });
    expect(instance.gameManager?.getFrameNum).toBeUndefined();
  });

  it("only projects the versioned RPG position probe for RPG adapters", () => {
    const position = {mapId: 3, playerX: 7, playerY: 9, fixtureState: 1};
    const runtime = runtimeFixture({
      getCapabilities: () => ({...capabilities, frameCounter: true}),
      getFrameCount: () => 302,
      getValidationProbe: (kind) => kind === "rpgmaker.position.v1"
        ? {kind, schemaVersion: 1, value: position}
        : null,
    });
    const instance = retromRuntimePlayerInstance(runtime, document.createElement("div"), {
      checkpointFormat: "mkxp-state-compact-v1",
      payloadKind: "RUNTIME_STATE",
      rpgPositionProbe: true,
    });

    expect(instance.gameManager?.getFrameNum?.()).toBe(302);
    expect(instance.gameManager?.getRpgPosition?.()).toEqual(position);
  });

  it("fails closed when checkpoint format or RPG probe shape drifts", async () => {
    const runtime = runtimeFixture({
      checkpoint: vi.fn(async () => ({bytes: new Uint8Array([1]), format: "wrong-format"})),
      getValidationProbe: (kind) => ({kind, schemaVersion: 1, value: {mapId: "1"}}),
    });
    const instance = retromRuntimePlayerInstance(runtime, document.createElement("div"), {
      checkpointFormat: "native-save-bundle-v1",
      payloadKind: "NATIVE_SAVE_BUNDLE_V1",
      rpgPositionProbe: true,
    });

    await expect(instance.gameManager?.getStateAsync?.()).rejects.toThrow("PLAYER_STATE_UNAVAILABLE");
    expect(() => instance.gameManager?.getRpgPosition?.()).toThrow("RPG_RUNTIME_POSITION_UNAVAILABLE");
  });
});

const capabilities: RuntimeCapabilities = {
  checkpoint: true,
  contentSources: ["FILE_TREE_V1"],
  frameCounter: false,
  pause: true,
  screenshot: true,
  standardGamepad: true,
  validationProbes: [],
  volume: false,
};

function runtimeFixture(overrides: Partial<GameRuntime>): GameRuntime {
  return {
    mount: vi.fn(async () => undefined),
    pause: vi.fn(async () => undefined),
    resume: vi.fn(async () => undefined),
    checkpoint: vi.fn(async () => ({bytes: new Uint8Array([1]), format: "native-save-bundle-v1"})),
    screenshot: vi.fn(async () => new Blob()),
    exit: vi.fn(async () => undefined),
    getState: () => "RUNNING",
    getCapabilities: () => capabilities,
    getCheckpointAvailability: () => ({available: true, blocker: null}),
    getCanvas: () => null,
    getFrameCount: () => null,
    getValidationProbe: () => null,
    setVolume: vi.fn(),
    subscribe: vi.fn(() => vi.fn()),
    ...overrides,
  };
}
