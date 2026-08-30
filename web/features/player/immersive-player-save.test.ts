import { describe, expect, it, vi } from "vitest";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
import { saveImmersivePlayerState } from "./immersive-player-save";

function emulator(state: Uint8Array, screenshot = new Blob(["cover"], { type: "image/png" })) {
  return {
    on: () => undefined,
    gameManager: { getState: () => state },
    takeScreenshot: vi.fn(async () => ({ blob: screenshot, format: "png" })),
  } satisfies EmulatorInstance;
}

describe("saveImmersivePlayerState", () => {
  it("captures and uploads through the existing manual-save product chain", async () => {
    const instance = emulator(Uint8Array.of(1, 2, 3));
    const upload = vi.fn(async () => true);
    await expect(saveImmersivePlayerState(instance, true, upload)).resolves.toBe(true);
    expect(instance.takeScreenshot).toHaveBeenCalledWith("canvas", "png", 1);
    expect(upload).toHaveBeenCalledOnce();
    expect(upload).toHaveBeenCalledWith(expect.objectContaining({ format: "png", state: Uint8Array.of(1, 2, 3) }));
  });

  it("reuses the screenshot requested before the immersive menu paused the core", async () => {
    const instance = emulator(Uint8Array.of(1, 2, 3));
    const prepared = new Blob(["running-frame"], { type: "image/png" });
    const upload = vi.fn(async () => true);
    await expect(saveImmersivePlayerState(
      instance,
      true,
      upload,
      Promise.resolve({ screenshot: prepared, format: "png" }),
    )).resolves.toBe(true);
    expect(instance.takeScreenshot).not.toHaveBeenCalled();
    expect(upload).toHaveBeenCalledWith(expect.objectContaining({ screenshot: prepared }));
  });

  it("uploads the valid state without a screenshot when the optional preview capture fails", async () => {
    const instance = emulator(Uint8Array.of(1, 2, 3));
    instance.takeScreenshot.mockRejectedValueOnce(new Error("capture failed"));
    const upload = vi.fn(async () => true);

    await expect(saveImmersivePlayerState(instance, true, upload)).resolves.toBe(true);
    expect(upload).toHaveBeenCalledWith(expect.objectContaining({
      screenshot: expect.objectContaining({ size: 0 }),
      state: Uint8Array.of(1, 2, 3),
    }));
  });

  it("does not capture or upload when recoverable saves are unavailable", async () => {
    const instance = emulator(Uint8Array.of(1));
    const upload = vi.fn(async () => true);
    await expect(saveImmersivePlayerState(instance, false, upload)).resolves.toBe(false);
    expect(instance.takeScreenshot).not.toHaveBeenCalled();
    expect(upload).not.toHaveBeenCalled();
  });

  it("reports capture, state, and upload failures without leaving the menu", async () => {
    const missingState = emulator(new Uint8Array());
    const upload = vi.fn(async () => true);
    await expect(saveImmersivePlayerState(missingState, true, upload)).resolves.toBe(false);
    expect(upload).not.toHaveBeenCalled();
    await expect(saveImmersivePlayerState(emulator(Uint8Array.of(1)), true, async () => false)).resolves.toBe(false);
  });
});
