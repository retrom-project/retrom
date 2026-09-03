import {describe, expect, it, vi} from "vitest";

import type {PlayerRuntimeV1} from "./contract";
import {captureRuntimeSave, setRuntimePaused, switchRuntimeDisc} from "./runtime-actions";

describe("Provider runtime actions", () => {
  it("captures the Provider checkpoint and screenshot as one opaque save", async () => {
    const runtime = fixtureRuntime();
    const payload = await captureRuntimeSave(runtime);

    expect(payload.checkpoint).toEqual({
      bytes: new Uint8Array([1, 2, 3]), format: "fixture-checkpoint-v1", metadata: {slot: 2},
    });
    expect(payload.screenshot.type).toBe("image/png");
    expect(runtime.checkpoint).toHaveBeenCalledOnce();
    expect(runtime.screenshot).toHaveBeenCalledOnce();
  });

  it("uses the standard pause and disc APIs", async () => {
    const runtime = fixtureRuntime();
    await setRuntimePaused(runtime, true);
    await setRuntimePaused(runtime, false);
    await expect(switchRuntimeDisc(runtime, 1)).resolves.toEqual({
      count: 2, currentIndex: 1, labels: ["Disc 1", "Disc 2"],
    });
    expect(runtime.pause).toHaveBeenCalledOnce();
    expect(runtime.resume).toHaveBeenCalledOnce();
    expect(runtime.switchDisc).toHaveBeenCalledWith(1);
  });
});

function fixtureRuntime() {
  return {
    checkpoint: vi.fn(async () => ({
      bytes: new Uint8Array([1, 2, 3]), format: "fixture-checkpoint-v1", metadata: {slot: 2},
    })),
    screenshot: vi.fn(async () => new Blob([new Uint8Array([137, 80])], {type: "image/png"})),
    pause: vi.fn(async () => undefined),
    resume: vi.fn(async () => undefined),
    switchDisc: vi.fn(async () => ({count: 2, currentIndex: 1, labels: ["Disc 1", "Disc 2"]})),
  } as unknown as PlayerRuntimeV1;
}
