import type { Schema } from "@/lib/api/types";
import type {
  PlayerRuntimeV1,
  RuntimeCheckpointV1,
  RuntimeStateV1,
} from "./runtime/contract";
export const checkpoint: RuntimeCheckpointV1 = {
  format: "native-save",
  bytes: new Uint8Array([1, 2, 3]),
  metadata: { revision: "original" },
};
export function runFixture(): Schema<"Run"> {
  return {
    id: "018f0f31-26fe-7a31-9d61-4ec92f16d4c3",
    gameId: "018f0f31-26fe-7a31-9d61-4ec92f16d4c4",
    purpose: "play",
    coreId: "original-core",
    providerId: "fixture",
    targetId: "fixture",
    coreFingerprint: "a".repeat(64),
    romHash: "b".repeat(64),
    providerModuleUrl: "/runtime/providers/fixture/client.mjs",
    expiresAtMs: Date.now() + 60000,
    envelope: { session: { title: "原始游戏" } },
    save: null,
    extinfo: {
      coreId: "original-core",
      providerId: "fixture",
      targetId: "fixture",
      coreFingerprint: "a".repeat(64),
      romHash: "b".repeat(64),
      checkpointFormat: "native-save",
      runtimeOptions: { original: "value" },
      content: { kind: "SINGLE_FILE", entryFile: "original.rom" },
    },
  };
}
export function runtimeFixture(
  state: RuntimeStateV1 = "RUNNING",
): PlayerRuntimeV1 {
  return {
    mount: async () => undefined,
    pause: async () => undefined,
    resume: async () => undefined,
    checkpoint: async () => checkpoint,
    acknowledgeCheckpoint: async () => undefined,
    screenshot: async () => new Blob(["png"]),
    setVolume: async () => undefined,
    setVideoMode: async () => undefined,
    openNativeSettings: async () => undefined,
    closeNativeSettings: async () => undefined,
    setInputFilter: async () => undefined,
    getState: () => state,
    getCapabilities: () => ({
      checkpoint: true,
      pause: true,
      screenshot: true,
      standardGamepad: true,
      frameCounter: false,
      volume: true,
      nativeSettings: false,
      inputFilter: false,
      videoModes: ["original"],
      requiresThreads: false,
      frameMode: "NONE",
    }),
    getInputCapabilities: () => ({ hostShortcuts: [] }),
    getCheckpointAvailability: () => ({
      available: true,
      reason: null,
      revision: "native-dirty",
    }),
    getCanvas: () => null,
    getFrameCount: () => null,
    subscribe: () => () => undefined,
    exit: async () => undefined,
  };
}
