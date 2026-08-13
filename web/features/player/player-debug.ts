import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";

export type PlayerDebugSample = {
  frameCount: number | null;
  sampledAtMs: number;
};

export type PlayerDebugMetrics = {
  fps: number | null;
  frameCount: number | null;
  canvasWidth: number | null;
  canvasHeight: number | null;
  viewportWidth: number;
  viewportHeight: number;
  devicePixelRatio: number;
};

function boundedRuntimeNumber(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : null;
}

function readFrameCount(instance: EmulatorInstance | undefined) {
  try {
    return boundedRuntimeNumber(instance?.gameManager?.getFrameNum?.());
  } catch {
    return null;
  }
}

export function samplePlayerDebugMetrics(
  instance: EmulatorInstance | undefined,
  canvas: HTMLCanvasElement | null,
  previous: PlayerDebugSample | null,
  sampledAtMs: number,
  viewport: { width: number; height: number; devicePixelRatio: number },
): { metrics: PlayerDebugMetrics; sample: PlayerDebugSample } {
  const frameCount = readFrameCount(instance);
  const elapsedMs = previous ? sampledAtMs - previous.sampledAtMs : 0;
  const frameDelta = previous?.frameCount !== null && previous?.frameCount !== undefined && frameCount !== null
    ? frameCount - previous.frameCount
    : -1;
  const rawFPS = elapsedMs > 0 && frameDelta >= 0 ? frameDelta * 1_000 / elapsedMs : null;
  const fps = rawFPS === null || !Number.isFinite(rawFPS) ? null : Math.round(rawFPS * 10) / 10;
  const width = boundedRuntimeNumber(canvas?.width);
  const height = boundedRuntimeNumber(canvas?.height);
  return {
    metrics: {
      fps,
      frameCount,
      canvasWidth: width && width > 0 ? width : null,
      canvasHeight: height && height > 0 ? height : null,
      viewportWidth: Math.max(0, Math.round(viewport.width)),
      viewportHeight: Math.max(0, Math.round(viewport.height)),
      devicePixelRatio: Math.max(0, viewport.devicePixelRatio),
    },
    sample: { frameCount, sampledAtMs },
  };
}
