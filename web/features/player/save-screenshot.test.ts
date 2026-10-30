import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { prepareSaveScreenshot } from "./save-screenshot";

const jpeg = new Blob(["encoded"], { type: "image/jpeg" });
const close = vi.fn();
const context = {
  fillStyle: "",
  fillRect: vi.fn(),
  drawImage: vi.fn(),
  imageSmoothingEnabled: false,
  imageSmoothingQuality: "low",
};

beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal("createImageBitmap", vi.fn(async () => ({ width: 1920, height: 1080, close })));
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(context as unknown as CanvasRenderingContext2D);
});
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("save screenshot encoding", () => {
  it.each([
    [1920, 1080, 960, 540],
    [1080, 1920, 304, 540],
    [320, 240, 320, 240],
    [960, 540, 960, 540],
    [1, 1080, 1, 540],
  ])("encodes %i × %i as JPEG at %i × %i without upscaling", async (width, height, expectedWidth, expectedHeight) => {
    vi.mocked(createImageBitmap).mockResolvedValue({ width, height, close } as unknown as ImageBitmap);
    const encode = vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation(function (this: HTMLCanvasElement, callback) {
      expect([this.width, this.height]).toEqual([expectedWidth, expectedHeight]);
      callback(jpeg);
    });
    expect(await prepareSaveScreenshot(new Blob(["source"], { type: "image/png" }))).toBe(jpeg);
    expect(context.fillStyle).toBe("#000");
    expect(context.fillRect).toHaveBeenCalledWith(0, 0, expectedWidth, expectedHeight);
    expect(context.drawImage).toHaveBeenCalledWith(expect.objectContaining({ width, height }), 0, 0, expectedWidth, expectedHeight);
    expect(context.imageSmoothingEnabled).toBe(true);
    expect(context.imageSmoothingQuality).toBe("high");
    expect(encode).toHaveBeenCalledWith(expect.any(Function), "image/jpeg", 0.95);
    expect(close).toHaveBeenCalledOnce();
  });

  it("leaves an omitted screenshot absent", async () => {
    expect(await prepareSaveScreenshot(null)).toBeNull();
    expect(createImageBitmap).not.toHaveBeenCalled();
  });

  it("omits an undecodable image without blocking the checkpoint", async () => {
    vi.mocked(createImageBitmap).mockRejectedValue(new Error("decode failed"));
    expect(await prepareSaveScreenshot(new Blob(["invalid"]))).toBeNull();
    expect(close).not.toHaveBeenCalled();
  });

  it("releases decoded pixels when a drawing context is unavailable", async () => {
    vi.mocked(HTMLCanvasElement.prototype.getContext).mockReturnValue(null);
    expect(await prepareSaveScreenshot(new Blob(["source"]))).toBeNull();
    expect(close).toHaveBeenCalledOnce();
  });

  it.each([null, new Blob(), new Blob(["png"], { type: "image/png" })])("omits a failed or unsupported JPEG encoding", async (encoded) => {
    vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation(callback => callback(encoded));
    expect(await prepareSaveScreenshot(new Blob(["source"]))).toBeNull();
    expect(close).toHaveBeenCalledOnce();
  });

  it("releases decoded pixels when the encoder throws", async () => {
    vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation(() => { throw new Error("encode failed"); });
    expect(await prepareSaveScreenshot(new Blob(["source"]))).toBeNull();
    expect(close).toHaveBeenCalledOnce();
  });
});
