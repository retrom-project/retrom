import { describe, expect, it, vi } from "vitest";
import {
  manualSaveScreenshotJpegQuality,
  maximumManualSaveScreenshotBytes,
  maximumManualSaveScreenshotHeight,
  maximumManualSaveScreenshotWidth,
  prepareManualSaveScreenshot,
} from "./manual-save-screenshot";

describe("manual save screenshot upload boundary", () => {
  it("re-encodes an already small capture as a bounded JPEG preview", async () => {
    const capture = { screenshot: new Blob(["png"], { type: "image/png" }), format: "png" };
    const bitmap = { width: 640, height: 360, close: vi.fn() } as unknown as ImageBitmap;
    const canvas = document.createElement("canvas");
    const drawImage = vi.fn();
    vi.spyOn(canvas, "getContext").mockReturnValue({ drawImage } as unknown as CanvasRenderingContext2D);
    const toBlob = vi.spyOn(canvas, "toBlob").mockImplementation((callback) => {
      callback(new Blob(["jpeg"], { type: "image/jpeg" }));
    });

    const result = await prepareManualSaveScreenshot(capture, {
      createBitmap: vi.fn(async () => bitmap),
      createCanvas: () => canvas,
    });

    expect(canvas.width).toBe(640);
    expect(canvas.height).toBe(360);
    expect(drawImage).toHaveBeenCalledWith(bitmap, 0, 0, 640, 360);
    expect(toBlob).toHaveBeenCalledWith(expect.any(Function), "image/jpeg", manualSaveScreenshotJpegQuality);
    expect(result?.screenshot.type).toBe("image/jpeg");
    expect(result?.format).toBe("jpg");
    expect(bitmap.close).toHaveBeenCalled();
  });

  it("downscales a high-DPI fullscreen capture before upload", async () => {
    const bitmap = { width: 3840, height: 2160, close: vi.fn() } as unknown as ImageBitmap;
    const canvas = document.createElement("canvas");
    const drawImage = vi.fn();
    vi.spyOn(canvas, "getContext").mockReturnValue({ drawImage } as unknown as CanvasRenderingContext2D);
    const toBlob = vi.spyOn(canvas, "toBlob").mockImplementation((callback) => {
      callback(new Blob([new Uint8Array(512 * 1024)], { type: "image/jpeg" }));
    });

    const result = await prepareManualSaveScreenshot({
      screenshot: new Blob([new Uint8Array(maximumManualSaveScreenshotBytes + 1)], { type: "image/png" }),
      format: "png",
    }, {
      createBitmap: vi.fn(async () => bitmap),
      createCanvas: () => canvas,
    });

    expect(canvas.width).toBe(maximumManualSaveScreenshotWidth);
    expect(canvas.height).toBe(maximumManualSaveScreenshotHeight);
    expect(drawImage).toHaveBeenCalledWith(
      bitmap,
      0,
      0,
      maximumManualSaveScreenshotWidth,
      maximumManualSaveScreenshotHeight,
    );
    expect(toBlob).toHaveBeenCalledWith(expect.any(Function), "image/jpeg", manualSaveScreenshotJpegQuality);
    expect(result?.screenshot.size).toBe(512 * 1024);
    expect(result?.screenshot.type).toBe("image/jpeg");
    expect(result?.format).toBe("jpg");
    expect(bitmap.close).toHaveBeenCalled();
  });

  it("omits only the optional screenshot when conversion cannot satisfy the limit", async () => {
    const bitmap = { width: 3840, height: 2160, close: vi.fn() } as unknown as ImageBitmap;
    const canvas = document.createElement("canvas");
    vi.spyOn(canvas, "getContext").mockReturnValue({ drawImage: vi.fn() } as unknown as CanvasRenderingContext2D);
    vi.spyOn(canvas, "toBlob").mockImplementation((callback) => {
      callback(new Blob([new Uint8Array(maximumManualSaveScreenshotBytes + 1)], { type: "image/jpeg" }));
    });

    await expect(prepareManualSaveScreenshot({
      screenshot: new Blob([new Uint8Array(maximumManualSaveScreenshotBytes + 1)], { type: "image/png" }),
      format: "png",
    }, {
      createBitmap: vi.fn(async () => bitmap),
      createCanvas: () => canvas,
    })).resolves.toBeNull();
    expect(bitmap.close).toHaveBeenCalled();
  });
});
