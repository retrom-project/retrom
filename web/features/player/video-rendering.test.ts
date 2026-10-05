import {afterEach, describe, expect, it, vi} from "vitest";
import type {PlayerRuntimeV1, RuntimeVideoModeV1} from "./runtime/contract";
import {
  applyVideoRenderingMode, readVideoRenderingMode, subscribeVideoRenderingMode,
  videoRenderingModeOptions, writeVideoRenderingMode,
} from "./video-rendering";

afterEach(() => window.localStorage.clear());

describe("Player video rendering modes", () => {
  it("defaults to sharp pixels and exposes only contract modes", () => {
    expect(readVideoRenderingMode("user-1")).toBe("pixel");
    expect(videoRenderingModeOptions.map((option) => option.value)).toEqual([
      "sharp-bilinear", "pixel", "adaptive-sharpen", "smooth", "original",
    ]);
  });

  it("stores and publishes authenticated-user preference changes", () => {
    const listener = vi.fn();
    const unsubscribe = subscribeVideoRenderingMode(listener);
    writeVideoRenderingMode("user-1", "adaptive-sharpen");
    expect(window.localStorage.getItem("retrom:v2:user:user-1:player:video-rendering-mode")).toBe("adaptive-sharpen");
    expect(readVideoRenderingMode("user-1")).toBe("adaptive-sharpen");
    expect(listener).toHaveBeenCalledOnce();
    unsubscribe();
  });

  it("delegates supported modes to PlayerRuntimeV1", () => {
    const setVideoMode = vi.fn(async () => undefined);
    const runtime = {
      getCapabilities: () => ({videoModes: ["pixel", "smooth"] as RuntimeVideoModeV1[]}), setVideoMode,
    } as unknown as PlayerRuntimeV1;
    expect(applyVideoRenderingMode(runtime, "smooth")).toBe(true);
    expect(setVideoMode).toHaveBeenCalledWith("smooth");
    expect(applyVideoRenderingMode(runtime, "original")).toBe(false);
    expect(setVideoMode).toHaveBeenCalledOnce();
  });
});
