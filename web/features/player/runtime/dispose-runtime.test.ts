import { describe, it, expect, vi } from "vitest";
import { disposeRuntime } from "./dispose-runtime";
import { runtimeFixture, checkpoint } from "../player-test-fixture";
describe("runtime teardown", () => {
  it("waits for native IO before final export, persistence and core release", async () => {
    vi.useFakeTimers();
    try {
      const runtime = runtimeFixture(), order: string[] = [];
      const availability = vi.spyOn(runtime, "getCheckpointAvailability").mockReturnValue({ available: false, reason: "BUSY" });
      runtime.checkpoint = vi.fn(async () => { order.push("export"); return checkpoint; });
      runtime.exit = vi.fn(async () => { order.push("exit"); });
      const disposing = disposeRuntime(runtime, async () => { order.push("persist"); });
      await vi.advanceTimersByTimeAsync(100);
      expect(order).toEqual([]);
      availability.mockReturnValue({ available: true, reason: null, revision: "native-complete" });
      await vi.advanceTimersByTimeAsync(50);
      await disposing;
      expect(order).toEqual(["export", "persist", "exit"]);
    } finally { vi.useRealTimers(); }
  });
  it("exports dirty native files before persistence and core teardown", async () => {
    const order: string[] = [];
    const runtime = runtimeFixture();
    runtime.checkpoint = vi.fn(async () => {
      order.push("export");
      return checkpoint;
    });
    runtime.exit = vi.fn(async () => {
      order.push("exit");
    });
    await disposeRuntime(runtime, async () => {
      order.push("persist");
    });
    expect(order).toEqual(["export", "persist", "exit"]);
    expect(runtime.checkpoint).toHaveBeenCalledWith({ intent: "EXPORT" });
  });
  it("omits the screenshot when the final export adds no player save point", async () => {
    const runtime = runtimeFixture();
    runtime.checkpoint = vi.fn(async () => ({ ...checkpoint, screenshot: "UNCHANGED" as const }));
    const screenshot = vi.spyOn(runtime, "screenshot");
    const persisted = vi.fn(async () => undefined);
    await disposeRuntime(runtime, persisted);
    expect(screenshot).not.toHaveBeenCalled();
    expect(persisted).toHaveBeenCalledWith(expect.objectContaining({ screenshot: null }));
  });
  it("uses the save-point image a checkpoint carries instead of the current frame", async () => {
    const runtime = runtimeFixture();
    const image = new Blob(["save-point"], { type: "image/png" });
    runtime.checkpoint = vi.fn(async () => ({ ...checkpoint, screenshot: image }));
    const screenshot = vi.spyOn(runtime, "screenshot");
    const persisted = vi.fn(async () => undefined);
    await disposeRuntime(runtime, persisted);
    expect(screenshot).not.toHaveBeenCalled();
    expect(persisted).toHaveBeenCalledWith(expect.objectContaining({ screenshot: image }));
  });
  it("does not capture an unmounted StrictMode instance", async () => {
    const runtime = runtimeFixture("CREATED");
    const capture = vi.spyOn(runtime, "checkpoint");
    await disposeRuntime(runtime, async () => undefined);
    expect(capture).not.toHaveBeenCalled();
  });
  it("reports persistence failure while still releasing the core", async () => {
    const runtime = runtimeFixture();
    const exit = vi.spyOn(runtime, "exit");
    await expect(
      disposeRuntime(runtime, async () => {
        throw new Error("disk full");
      }),
    ).rejects.toThrow("disk full");
    expect(exit).toHaveBeenCalledOnce();
  });
});
