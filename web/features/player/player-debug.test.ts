import {describe, expect, it} from "vitest";
import { samplePlayerDebugMetrics } from "./player-debug";
import type {PlayerRuntimeV1} from "./runtime/contract";

function runtime(frame: number | (() => number)): PlayerRuntimeV1 {
  return {getFrameCount: typeof frame === "function" ? frame : () => frame} as PlayerRuntimeV1;
}

describe("samplePlayerDebugMetrics", () => {
  it("calculates the emulated frame rate from consecutive core frame counters", () => {
    const canvas = document.createElement("canvas");
    canvas.width = 320;
    canvas.height = 240;
    const first = samplePlayerDebugMetrics(runtime(120), canvas, null, 1_000, { width: 1440, height: 900, devicePixelRatio: 2 });
    const second = samplePlayerDebugMetrics(runtime(180), canvas, first.sample, 2_000, { width: 1440, height: 900, devicePixelRatio: 2 });

    expect(first.metrics.fps).toBeNull();
    expect(second.metrics).toMatchObject({ fps: 60, frameCount: 180, canvasWidth: 320, canvasHeight: 240, viewportWidth: 1440, viewportHeight: 900, devicePixelRatio: 2 });
  });

  it("reports zero FPS while paused and contains unsupported runtime values", () => {
    const previous = { frameCount: 55, sampledAtMs: 1_000 };
    const paused = samplePlayerDebugMetrics(runtime(55), null, previous, 2_250, { width: 800.4, height: 599.6, devicePixelRatio: 1 });
    const unavailable = samplePlayerDebugMetrics(runtime(() => { throw new Error("not ready"); }), null, null, 3_000, { width: 800, height: 600, devicePixelRatio: 1 });

    expect(paused.metrics).toMatchObject({ fps: 0, frameCount: 55, canvasWidth: null, canvasHeight: null, viewportWidth: 800, viewportHeight: 600 });
    expect(unavailable.metrics.frameCount).toBeNull();
    expect(unavailable.metrics.fps).toBeNull();
  });
});
