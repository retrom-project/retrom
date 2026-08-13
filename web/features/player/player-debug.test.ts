import { describe, expect, it, vi } from "vitest";
import { samplePlayerDebugMetrics } from "./player-debug";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";

function instance(frame: number | (() => number)): EmulatorInstance {
  return {
    on: vi.fn(),
    gameManager: { getFrameNum: typeof frame === "function" ? frame : () => frame },
  };
}

describe("samplePlayerDebugMetrics", () => {
  it("calculates the emulated frame rate from consecutive core frame counters", () => {
    const canvas = document.createElement("canvas");
    canvas.width = 320;
    canvas.height = 240;
    const first = samplePlayerDebugMetrics(instance(120), canvas, null, 1_000, { width: 1440, height: 900, devicePixelRatio: 2 });
    const second = samplePlayerDebugMetrics(instance(180), canvas, first.sample, 2_000, { width: 1440, height: 900, devicePixelRatio: 2 });

    expect(first.metrics.fps).toBeNull();
    expect(second.metrics).toMatchObject({ fps: 60, frameCount: 180, canvasWidth: 320, canvasHeight: 240, viewportWidth: 1440, viewportHeight: 900, devicePixelRatio: 2 });
  });

  it("reports zero FPS while paused and contains unsupported runtime values", () => {
    const previous = { frameCount: 55, sampledAtMs: 1_000 };
    const paused = samplePlayerDebugMetrics(instance(55), null, previous, 2_250, { width: 800.4, height: 599.6, devicePixelRatio: 1 });
    const unavailable = samplePlayerDebugMetrics(instance(() => { throw new Error("not ready"); }), null, null, 3_000, { width: 800, height: 600, devicePixelRatio: 1 });

    expect(paused.metrics).toMatchObject({ fps: 0, frameCount: 55, canvasWidth: null, canvasHeight: null, viewportWidth: 800, viewportHeight: 600 });
    expect(unavailable.metrics.frameCount).toBeNull();
    expect(unavailable.metrics.fps).toBeNull();
  });
});
