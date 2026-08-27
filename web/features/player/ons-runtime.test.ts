import { describe, expect, it, vi } from "vitest";

import { isOnsLaunchConfig, onsPlayerInstance, validateOnsLaunchConfig } from "./ons-runtime";

const launchId = "01980000-0000-7000-8000-000000000001";
const artifactId = "01980000-0000-7000-8000-000000000002";

function config() {
  return {
    runtimeFamily: "ONS", protocolVersion: 1, mode: "single", purpose: "PRODUCT",
    launchId, sessionId: launchId, coreId: "onscripter_yuri", coreName: "ONScripterYuri",
    gameTitle: "Fixture", platformName: "ONScripter", runtimeVersion: "v0.3.0", artifactId,
    returnTo: "/games/fixture", warnings: [], checkpoint: null,
    adapter: {
      adapterKind: "ONS_YURI_WEB", adapterId: "ons-yuri-web",
      runtimeBaseUrl: "/runtime/retrom-runtime/v0.3.0/",
      projectIndexUrl: `/runtime/projects/${launchId}/index.json`, scriptEncoding: "utf8", checkpointSlot: 999,
    },
  };
}

describe("ONS product runtime", () => {
  it("accepts the exact product launch contract", () => {
    expect(isOnsLaunchConfig(config())).toBe(true);
    const restoring = { ...config(), checkpoint: {
      payloadKind: "ONS_SAVE_BUNDLE_V1", payloadUrl: `/runtime/launches/${launchId}/state`,
    } };
    expect(() => validateOnsLaunchConfig(restoring)).not.toThrow();
  });

  it("rejects review, extra fields, route drift, and incompatible checkpoint payloads", () => {
    expect(isOnsLaunchConfig({ ...config(), purpose: "REVIEW_PREVIEW" })).toBe(false);
    expect(isOnsLaunchConfig({ ...config(), extra: true })).toBe(false);
    expect(isOnsLaunchConfig({ ...config(), adapter: { ...config().adapter, projectIndexUrl: "/other" } })).toBe(false);
    expect(isOnsLaunchConfig({ ...config(), checkpoint: {
      payloadKind: "RUNTIME_STATE", payloadUrl: `/runtime/launches/${launchId}/state`,
    } })).toBe(false);
  });

  it("bridges checkpoint, screenshot, pause, and resume into the shared Player shell", async () => {
    document.body.innerHTML = `<div id="target"><canvas width="640" height="480"></canvas></div>`;
    const runtime = {
      checkpoint: vi.fn(async () => ({ bytes: new Uint8Array([1, 2, 3]), payloadKind: "ONS_SAVE_BUNDLE_V1" as const })),
      screenshot: vi.fn(async () => new Blob(["png"], { type: "image/png" })),
      pause: vi.fn(async () => undefined), resume: vi.fn(async () => undefined),
      getCheckpointAvailability: vi.fn(() => ({ available: true, reason: null })),
    };
    const target = document.querySelector<HTMLElement>("#target")!;
    const instance = onsPlayerInstance(runtime as never, target);
    expect(instance.canvas).toBe(target.querySelector("canvas"));
    expect(await instance.gameManager?.getStateAsync?.()).toEqual(new Uint8Array([1, 2, 3]));
    expect((await instance.takeScreenshot?.("canvas", "png", 1))?.blob.size).toBe(3);
    expect(instance.gameManager?.savePayloadKind).toBe("ONS_SAVE_BUNDLE_V1");
    instance.gameManager?.toggleMainLoop?.(false);
    instance.gameManager?.toggleMainLoop?.(true);
    await Promise.resolve();
    expect(runtime.pause).toHaveBeenCalledOnce();
    expect(runtime.resume).toHaveBeenCalledOnce();
    expect(instance.gameManager?.getVideoDimensions?.("aspect")).toBeCloseTo(4 / 3);
  });
});
