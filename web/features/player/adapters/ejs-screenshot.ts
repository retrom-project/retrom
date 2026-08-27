import type { EmulatorInstance } from "./ejs-4.2.3-v2";

export type ManualScreenshot = { screenshot: Blob; format: string };

async function captureCanvasScreenshot(instance: EmulatorInstance): Promise<ManualScreenshot> {
  if (!instance.takeScreenshot) {throw new Error("PLAYER_SCREENSHOT_UNAVAILABLE");}
  const photo = instance.capture?.photo;
  const result = await instance.takeScreenshot(photo?.source ?? "canvas", photo?.format ?? "png", photo?.upscale ?? 1);
  if (!result.blob || typeof result.blob.size !== "number" || result.blob.size === 0) {
    throw new Error("PLAYER_SCREENSHOT_EMPTY");
  }
  return { screenshot: result.blob, format: result.format || "png" };
}

const reviewCoreScreenshotTimeoutMs = 2_000;
const screenshotSampleSide = 64;
const minimumVisiblePixelRatio = 0.01;
const minimumVisibleLuma = 8;

export function screenshotPixelsHaveVisibleContent(pixels: Uint8ClampedArray) {
  const pixelCount = pixels.length / 4;
  const visiblePixelTarget = Math.ceil(pixelCount * minimumVisiblePixelRatio);
  let visiblePixels = 0;
  for (let index = 0; index < pixels.length; index += 4) {
    const luma = (pixels[index]! + pixels[index + 1]! + pixels[index + 2]!) / 3;
    if (luma > minimumVisibleLuma && ++visiblePixels >= visiblePixelTarget) {return true;}
  }
  return false;
}

async function screenshotHasVisibleContent(screenshot: Blob) {
  if (typeof createImageBitmap !== "function") {return true;}
  let bitmap: ImageBitmap | null = null;
  try {
    bitmap = await createImageBitmap(screenshot);
    const sample = document.createElement("canvas");
    sample.width = screenshotSampleSide;
    sample.height = screenshotSampleSide;
    const context = sample.getContext("2d", { alpha: false });
    if (!context) {return false;}
    context.drawImage(bitmap, 0, 0, sample.width, sample.height);
    return screenshotPixelsHaveVisibleContent(
      context.getImageData(0, 0, sample.width, sample.height).data,
    );
  } catch {
    return false;
  } finally {
    bitmap?.close();
  }
}

export function coreFramebufferNeedsCanvasOrientation(bytes: Uint8Array, expectedAspect: number | undefined) {
  if (!Number.isFinite(expectedAspect) || !expectedAspect || expectedAspect <= 0 || bytes.byteLength < 24 ||
    bytes[0] !== 0x89 || bytes[1] !== 0x50 || bytes[2] !== 0x4e || bytes[3] !== 0x47 ||
    bytes[12] !== 0x49 || bytes[13] !== 0x48 || bytes[14] !== 0x44 || bytes[15] !== 0x52) {return false;}
  const dimensions = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const width = dimensions.getUint32(16);
  const height = dimensions.getUint32(20);
  if (!width || !height) {return false;}
  const directError = Math.abs(Math.log(width / height / expectedAspect));
  const rotatedError = Math.abs(Math.log(height / width / expectedAspect));
  return rotatedError + 0.05 < directError;
}

function readCoreScreenshot(instance: EmulatorInstance): ManualScreenshot | null {
  const fileSystem = instance.gameManager?.FS;
  if (!fileSystem?.readFile || !fileSystem.stat) {return null;}
  try {
    fileSystem.stat("/screenshot.png");
    const source = fileSystem.readFile("/screenshot.png");
    if (source.byteLength === 0) {return null;}
    const bytes = Uint8Array.from(new Uint8Array(source.buffer, source.byteOffset, source.byteLength));
    if (coreFramebufferNeedsCanvasOrientation(bytes, instance.gameManager?.getVideoDimensions?.("aspect"))) {
      throw new Error("PLAYER_CORE_SCREENSHOT_ORIENTATION_MISMATCH");
    }
    return { screenshot: new Blob([bytes], { type: "image/png" }), format: "png" };
  } catch (error) {
    if (error instanceof Error && error.message === "PLAYER_CORE_SCREENSHOT_ORIENTATION_MISMATCH") {throw error;}
    return null;
  }
}

async function captureCoreFramebuffer(instance: EmulatorInstance): Promise<ManualScreenshot> {
  const fileSystem = instance.gameManager?.FS;
  const requestScreenshot = instance.gameManager?.functions?.screenshot;
  if (!fileSystem?.readFile || !fileSystem.stat || !requestScreenshot) {
    throw new Error("PLAYER_CORE_SCREENSHOT_UNAVAILABLE");
  }
  try {fileSystem.unlink("/screenshot.png");} catch { /* The previous capture is optional. */ }
  requestScreenshot();
  const deadline = Date.now() + reviewCoreScreenshotTimeoutMs;
  while (Date.now() <= deadline) {
    const screenshot = readCoreScreenshot(instance);
    if (screenshot) {return screenshot;}
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error("PLAYER_CORE_SCREENSHOT_TIMEOUT");
}

// The core framebuffer avoids blank WebGL readback after a core stops presenting
// and avoids encoding a viewport-sized shader canvas on high-DPI displays.
export async function captureManualScreenshot(instance: EmulatorInstance): Promise<ManualScreenshot> {
  try {
    const coreFramebuffer = await captureCoreFramebuffer(instance);
    if (await screenshotHasVisibleContent(coreFramebuffer.screenshot)) {return coreFramebuffer;}
  } catch {
    // Canvas capture below is the bounded fallback for unavailable core output.
  }
  return captureCanvasScreenshot(instance);
}

export const captureReviewScreenshot = captureManualScreenshot;

export async function captureManualState(instance: EmulatorInstance, capture: ManualScreenshot) {
  const state = instance.gameManager?.getStateAsync
    ? await instance.gameManager.getStateAsync()
    : instance.gameManager?.getState?.();
  // The runtime lives in a same-origin iframe; realm-local instanceof checks
  // reject its otherwise valid Uint8Array and Blob values.
  if (!state || !ArrayBuffer.isView(state) || state.byteLength === 0) {throw new Error("PLAYER_STATE_UNAVAILABLE");}
  if (!capture.screenshot || typeof capture.screenshot.size !== "number" || capture.screenshot.size === 0) {
    throw new Error("PLAYER_SCREENSHOT_EMPTY");
  }
  return {
    ...capture,
    state: new Uint8Array(state),
    payloadKind: instance.gameManager?.savePayloadKind ?? "RUNTIME_STATE",
    validationPurpose: instance.gameManager?.validationPurpose ?? false,
  };
}
