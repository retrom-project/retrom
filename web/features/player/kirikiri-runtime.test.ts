import { describe, expect, it, vi } from "vitest";

import {
  isKiriKiriLaunchConfig,
  kirikiriPlayerInstance,
  validateKiriKiriLaunchConfig,
} from "./kirikiri-runtime";

const launchId = "01980000-0000-7000-8000-000000000011";
const artifactId = "01980000-0000-7000-8000-000000000012";

function config() {
  return {
    runtimeFamily: "KIRIKIRI", protocolVersion: 1, mode: "single", purpose: "PRODUCT",
    launchId, sessionId: launchId, coreId: "kirikiri2", coreName: "KiriKiri2",
    gameTitle: "KAG fixture", platformName: "KiriKiri", runtimeVersion: "v0.7.3", artifactId,
    returnTo: "/games/fixture", warnings: [], checkpoint: null,
    adapter: {
      adapterKind: "KIRIKIRI2_WEB", adapterId: "kirikiri2-web",
      runtimeBaseUrl: "/runtime/retrom-runtime/v0.7.3/",
      projectIndexUrl: `/runtime/content/project/${"a".repeat(64)}/index.json`, startupXp3Path: "data.xp3",
      checkpointSlot: 1999,
    },
  };
}

describe("KiriKiri product runtime", () => {
  it("accepts exact loose and XP3 launch contracts", () => {
    expect(isKiriKiriLaunchConfig(config())).toBe(true);
    expect(isKiriKiriLaunchConfig({
      ...config(), adapter: { ...config().adapter, startupXp3Path: null },
    })).toBe(true);
    const restoring = { ...config(), checkpoint: {
      payloadKind: "KIRIKIRI_SAVE_BUNDLE_V1", payloadUrl: `/runtime/launches/${launchId}/state`,
    } };
    expect(() => validateKiriKiriLaunchConfig(restoring)).not.toThrow();
  });

  it("rejects review, route drift, ambiguous paths, and incompatible checkpoint payloads", () => {
    expect(isKiriKiriLaunchConfig({ ...config(), purpose: "REVIEW_PREVIEW" })).toBe(false);
    expect(isKiriKiriLaunchConfig({ ...config(), extra: true })).toBe(false);
    expect(isKiriKiriLaunchConfig({
      ...config(), adapter: { ...config().adapter, projectIndexUrl: "/other" },
    })).toBe(false);
    expect(isKiriKiriLaunchConfig({
      ...config(), adapter: { ...config().adapter, startupXp3Path: "../data.xp3" },
    })).toBe(false);
    expect(isKiriKiriLaunchConfig({ ...config(), checkpoint: {
      payloadKind: "RUNTIME_STATE", payloadUrl: `/runtime/launches/${launchId}/state`,
    } })).toBe(false);
  });

  it("bridges semantic checkpoint and display controls into the Player shell", async () => {
    document.body.innerHTML = `<div id="target"><canvas width="1280" height="720"></canvas></div>`;
    const runtime = {
      checkpoint: vi.fn(async () => ({
        bytes: new Uint8Array([1, 2, 3]), format: "kirikiri-save-bundle-v1",
      })),
      screenshot: vi.fn(async () => new Blob(["png"], { type: "image/png" })),
      pause: vi.fn(async () => undefined), resume: vi.fn(async () => undefined),
      getCheckpointAvailability: vi.fn(() => ({ available: true, blocker: null })),
      getCapabilities: vi.fn(() => ({ frameCounter: false, volume: false })),
      getCanvas: vi.fn(() => target.querySelector("canvas")),
    };
    const target = document.querySelector<HTMLElement>("#target")!;
    const instance = kirikiriPlayerInstance(runtime as never, target);
    expect(instance.canvas).toBe(target.querySelector("canvas"));
    expect(await instance.gameManager?.getStateAsync?.()).toEqual(new Uint8Array([1, 2, 3]));
    expect((await instance.takeScreenshot?.("canvas", "png", 1))?.blob.size).toBe(3);
    expect(instance.gameManager?.savePayloadKind).toBe("KIRIKIRI_SAVE_BUNDLE_V1");
    instance.gameManager?.toggleMainLoop?.(false);
    instance.gameManager?.toggleMainLoop?.(true);
    await Promise.resolve();
    expect(runtime.pause).toHaveBeenCalledOnce();
    expect(runtime.resume).toHaveBeenCalledOnce();
    expect(instance.gameManager?.getVideoDimensions?.("aspect")).toBeCloseTo(16 / 9);
  });
});
