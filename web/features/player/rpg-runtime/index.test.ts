import { createRuntime, describeRuntime, type RpgMakerRuntimeConfig } from "@xxxsen/retrom-runtime";
import { describe, expect, it, vi } from "vitest";

import { rpgValidationGates } from "../rpg-validation-protocol";
import {
  createRetromRpgRuntime,
  describeRetromRpgRuntime,
  type RpgRuntimeConfig,
} from ".";

vi.mock("@xxxsen/retrom-runtime", () => ({
  createRuntime: vi.fn(() => ({ runtime: true })),
  describeRuntime: vi.fn(() => ({
    crossOriginFrame: true,
    requiresThreads: false,
    runtimeBaseUrl: "https://runtime.example.test",
  })),
  mountRuntime: vi.fn(),
}));

describe("Retrom runtime host boundary", () => {
  it("maps the product config to the host-independent package config", () => {
    const config = nativeConfig();
    expect(describeRetromRpgRuntime(config)).toEqual({
      crossOriginFrame: true,
      requiresThreads: false,
      runtimeBaseUrl: "https://runtime.example.test",
    });
    expect(vi.mocked(describeRuntime)).toHaveBeenCalledWith({
      sessionId: launchId,
      generation: "RPGMV",
      validationPurpose: false,
      expectedRestorePosition: null,
      adapter: {
        ...config.adapter,
        cleanupUrl: "https://runtime.example.test/__retrom/cleanup",
      },
    });
  });

  it("passes server-recorded B and preserves the runtime diagnostic event", () => {
    const config = restoreConfig();
    const received = vi.fn();
    window.addEventListener("retrom:runtime-diagnostic", received);
    createRetromRpgRuntime(config, runtimeOptions());
    const options = vi.mocked(createRuntime).mock.calls.at(-1)?.[1];
    const runtimeConfig = vi.mocked(createRuntime).mock.calls.at(-1)?.[0] as RpgMakerRuntimeConfig;
    expect(runtimeConfig.expectedRestorePosition).toEqual(position);
    options?.onDiagnostic?.({ runtime: "mkxp-z", message: "[INFO] healthy" });
    expect(received).toHaveBeenCalledOnce();
    expect((received.mock.calls[0]?.[0] as CustomEvent).detail).toEqual({
      runtime: "mkxp-z",
      message: "[INFO] healthy",
    });
    window.removeEventListener("retrom:runtime-diagnostic", received);
  });
});

const launchId = "0198abcd-1234-7123-8abc-1234567890ab";
const originalLaunchId = "0198abcd-1234-7123-8abc-1234567890ae";
const position = { mapId: 7, playerX: 4, playerY: 6, fixtureState: 1 };

function nativeConfig(): RpgRuntimeConfig {
  const origin = "https://runtime.example.test";
  return {
    runtimeFamily: "RPGMAKER", protocolVersion: 1, mode: "single", purpose: "PRODUCT", launchId,
    coreId: "rpgmaker_mv", coreName: "RPG Maker MV", gameTitle: "Fixture", platformName: "RPG Maker",
    returnTo: "/games/fixture", warnings: [], generation: "RPGMV", routeKey: "RPGMV_NATIVE",
    artifactId: "0198abcd-1234-7123-8abc-1234567890ac", checkpoint: null,
    checkpointAvailability: { available: false, reason: "RUNTIME_NOT_READY" }, runtimeValidation: null,
    adapter: {
      adapterKind: "NATIVE_WEB", adapterId: "native-web", bridgeProfile: "RPGMV",
      uniqueOrigin: origin, bootstrapUrl: `${origin}/__retrom/bootstrap`, bootstrapTicket: "a".repeat(43),
    },
  };
}

function restoreConfig(): RpgRuntimeConfig {
  const config = nativeConfig();
  return {
    ...config,
    purpose: "RPG_RUNTIME_VALIDATION",
    checkpoint: { payloadKind: "NATIVE_SAVE_BUNDLE_V1", payloadUrl: `/runtime/launches/${launchId}/state` },
    runtimeValidation: {
      validationId: "0198abcd-1234-7123-8abc-1234567890ad", state: "RESTORED",
      originalLaunchId, restoreLaunchId: launchId, lastGateSequence: 2,
      machineGates: rpgValidationGates.map((gate) => gate === "SAVE_POINT_RECORDED" ? {
        gate, status: "PASSED", begunAtMs: 1, completedAtMs: 2, evidence: position, failureCode: null,
      } : {
        gate, status: "NOT_STARTED", begunAtMs: null, completedAtMs: null, evidence: null, failureCode: null,
      }),
      checkpointEvidence: null,
      restoreScreenshotUploaded: false,
    },
  };
}

function runtimeOptions() {
  const frame = document.createElement("iframe");
  return { frame, frameWindow: window, restorePayload: null };
}
