import { describe, it, expect, vi } from "vitest";
import { disposeRuntime } from "./dispose-runtime";
import { runtimeFixture, checkpoint } from "../player-test-fixture";
describe("runtime teardown", () => {
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
