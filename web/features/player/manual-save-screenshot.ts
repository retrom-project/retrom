import type { ManualScreenshot } from "./adapters/ejs-screenshot";

export const maximumManualSaveScreenshotBytes = 10 * 1024 * 1024;
export const maximumManualSaveScreenshotWidth = 640;
export const maximumManualSaveScreenshotHeight = 360;
export const manualSaveScreenshotJpegQuality = 0.75;

type ScreenshotPlatform = {
  createBitmap: (source: Blob) => Promise<ImageBitmap>;
  createCanvas: () => HTMLCanvasElement;
};

const browserScreenshotPlatform: ScreenshotPlatform = {
  createBitmap: (source) => createImageBitmap(source),
  createCanvas: () => document.createElement("canvas"),
};

export async function prepareManualSaveScreenshot(
  capture: ManualScreenshot,
  platform: ScreenshotPlatform = browserScreenshotPlatform,
): Promise<ManualScreenshot | null> {
  if (!capture.screenshot.size) {return null;}
  let bitmap: ImageBitmap | undefined;
  try {
    bitmap = await platform.createBitmap(capture.screenshot);
    if (!bitmap.width || !bitmap.height) {return null;}
    const scale = Math.min(
      1,
      maximumManualSaveScreenshotWidth / bitmap.width,
      maximumManualSaveScreenshotHeight / bitmap.height,
    );
    const canvas = platform.createCanvas();
    canvas.width = Math.max(1, Math.floor(bitmap.width * scale));
    canvas.height = Math.max(1, Math.floor(bitmap.height * scale));
    const context = canvas.getContext("2d");
    if (!context) {return null;}
    context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    const reduced = await canvasBlob(canvas);
    if (!reduced?.size || reduced.size > maximumManualSaveScreenshotBytes) {return null;}
    return { screenshot: reduced, format: "jpg" };
  } catch {
    return null;
  } finally {
    bitmap?.close();
  }
}

function canvasBlob(canvas: HTMLCanvasElement) {
  return new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/jpeg", manualSaveScreenshotJpegQuality));
}
