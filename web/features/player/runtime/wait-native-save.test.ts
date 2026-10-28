import { afterEach, expect, it, vi } from "vitest";
import { runtimeFixture } from "../player-test-fixture";
import { waitForNativeSave } from "./wait-native-save";

afterEach(() => vi.useRealTimers());
it("waits for native IO to complete before returning its exportable revision", async () => {
  vi.useFakeTimers();
  const runtime = runtimeFixture();
  const availability = vi.spyOn(runtime, "getCheckpointAvailability").mockReturnValue({ available: false, reason: "BUSY" });
  const waiting = waitForNativeSave(runtime);
  let finished = false;
  void waiting.then(() => { finished = true; });
  await vi.advanceTimersByTimeAsync(100);
  expect(finished).toBe(false);
  availability.mockReturnValue({ available: true, reason: null, revision: "complete" });
  await vi.advanceTimersByTimeAsync(50);
  await expect(waiting).resolves.toMatchObject({ available: true, revision: "complete" });
  expect(vi.getTimerCount()).toBe(0);
});
it("bounds an unfinished save and reports failed native IO", async () => {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "performance"] });
  const runtime = runtimeFixture();
  const availability = vi.spyOn(runtime, "getCheckpointAvailability").mockReturnValue({ available: false, reason: "BUSY" });
  const failed = expect(waitForNativeSave(runtime)).rejects.toThrow("游戏内存档仍在写入");
  await vi.advanceTimersByTimeAsync(10_000);
  await failed;
  availability.mockReturnValue({ available: false, reason: "FAILED" });
  await expect(waitForNativeSave(runtime)).rejects.toThrow("无法确认游戏内存档");
});
it("cancels an obsolete wait and clears its timer", async () => {
  vi.useFakeTimers();
  const runtime = runtimeFixture(), controller = new AbortController();
  vi.spyOn(runtime, "getCheckpointAvailability").mockReturnValue({ available: false, reason: "BUSY" });
  const failed = expect(waitForNativeSave(runtime, controller.signal)).rejects.toMatchObject({ name: "AbortError" });
  controller.abort();
  await failed;
  expect(vi.getTimerCount()).toBe(0);
});
it("allows an already failed runtime to release its resources", async () => {
  const runtime = runtimeFixture("FAILED");
  vi.spyOn(runtime, "getCheckpointAvailability").mockReturnValue({ available: false, reason: "FAILED" });
  await expect(waitForNativeSave(runtime)).resolves.toMatchObject({ available: false, reason: "FAILED" });
});
