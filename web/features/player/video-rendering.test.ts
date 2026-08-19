import { afterEach, describe, expect, it, vi } from "vitest";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
import {
  applyVideoRenderingMode,
  readVideoRenderingMode,
  subscribeVideoRenderingMode,
  videoRenderingModeOptions,
  writeVideoRenderingMode,
} from "./video-rendering";

afterEach(() => window.localStorage.clear());

describe("Player video rendering modes", () => {
  it("defaults to sharp pixels and keeps the selectable modes stable", () => {
    expect(readVideoRenderingMode("user-1")).toBe("pixel");
    expect(videoRenderingModeOptions.map((option) => option.value)).toEqual([
      "clear", "pixel", "sharpen", "smooth", "original",
    ]);
  });

  it("stores the preference in the authenticated user namespace", () => {
    writeVideoRenderingMode("user-1", "pixel");
    expect(window.localStorage.getItem("retrom:v2:user:user-1:player:video-rendering-mode")).toBe("pixel");
    expect(readVideoRenderingMode("user-1")).toBe("pixel");
    window.localStorage.setItem("retrom:v2:user:user-1:player:video-rendering-mode", "unknown");
    expect(readVideoRenderingMode("user-1")).toBe("pixel");
  });

  it("notifies same-page consumers when the preference changes", () => {
    const listener = vi.fn();
    const unsubscribe = subscribeVideoRenderingMode(listener);
    writeVideoRenderingMode("user-1", "sharpen");
    expect(listener).toHaveBeenCalledOnce();
    unsubscribe();
    writeVideoRenderingMode("user-1", "clear");
    expect(listener).toHaveBeenCalledOnce();
  });

  it.each([
    ["clear", "retrom-sharp-bilinear", "pixelated"],
    ["pixel", "disabled", "pixelated"],
    ["sharpen", "retrom-adaptive-sharpen", "auto"],
    ["smooth", "sabr", "auto"],
    ["original", "disabled", "auto"],
  ] as const)("applies %s to both the runtime shader and browser compositor", (mode, shader, imageRendering) => {
    const canvas = document.createElement("canvas");
    const changeSettingOption = vi.fn();
    const emulator: EmulatorInstance = { on: () => undefined, canvas, changeSettingOption };
    expect(applyVideoRenderingMode(emulator, canvas, mode)).toBe(true);
    expect(changeSettingOption).toHaveBeenCalledWith("shader", shader);
    expect(canvas.style.getPropertyValue("image-rendering")).toBe(imageRendering);
    expect(canvas.style.getPropertyPriority("image-rendering")).toBe("important");
  });

  it("still sharpens browser scaling while the runtime is not ready", () => {
    const canvas = document.createElement("canvas");
    expect(applyVideoRenderingMode(undefined, canvas, "clear")).toBe(false);
    expect(canvas.style.imageRendering).toBe("pixelated");
  });
});
