import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { usePlayerExit } from "./use-player-exit";
import { runtimeFixture } from "./player-test-fixture";
import type { RuntimeStateV1, RuntimeEventListenerV1 } from "./runtime/contract";
import type { GamepadFrameListener } from "@/features/immersive/gamepad-source";
import { browserGamepadSource } from "@/features/immersive/gamepad-source";
vi.mock("@/features/immersive/gamepad-source", () => ({ browserGamepadSource: { subscribe: vi.fn() } }));
let frame: GamepadFrameListener;
beforeEach(() => { vi.clearAllMocks(); vi.mocked(browserGamepadSource.subscribe).mockImplementation((listener) => { frame = listener; return vi.fn(); }); });
afterEach(cleanup);
function fixture(immersive = false, native = false, initial: RuntimeStateV1 = "RUNNING") {
  let state = initial;
  const instance = runtimeFixture();
  const listeners = new Set<RuntimeEventListenerV1>();
  vi.spyOn(instance, "subscribe").mockImplementation((listener) => { listeners.add(listener); return () => { listeners.delete(listener); }; });
  function transition(next: RuntimeStateV1) {
    const previous = state;
    state = next;
    for (const listener of listeners) { listener({ type: "STATE_CHANGED", previous, state: next }); }
  }
  vi.spyOn(instance, "getState").mockImplementation(() => state);
  const pause = vi.spyOn(instance, "pause").mockImplementation(async () => { state = "PAUSED"; });
  const resume = vi.spyOn(instance, "resume").mockImplementation(async () => { state = "RUNNING"; });
  const options = { runtime: { current: instance }, immersive, native, hasDraft: false, saving: false, retry: vi.fn(async () => true), capture: vi.fn(async () => true), leave: vi.fn(async () => {}), reportError: vi.fn() };
  return { options, pause, resume, instance, transition, listeners };
}
it.each(["RUNNING", "PAUSED"] as const)("normal cancel leaves %s game paused without closing the session", async (state) => {
  const f = fixture(false, false, state);
  const { result } = renderHook(() => usePlayerExit(f.options));
  await act(() => result.current.request());
  expect(f.instance.getState()).toBe("PAUSED");
  expect(f.pause).toHaveBeenCalledTimes(state === "RUNNING" ? 1 : 0);
  await act(() => result.current.cancel());
  expect(f.resume).not.toHaveBeenCalled();
  expect(result.current.open).toBe(false);
  expect(f.options.leave).not.toHaveBeenCalled();
});
it("instant create saves independently, keeps failures visible and never navigates", async () => {
  const f = fixture();
  f.options.capture.mockResolvedValueOnce(false).mockResolvedValueOnce(true);
  const { result } = renderHook(() => usePlayerExit(f.options));
  await act(() => result.current.request());
  await act(() => result.current.save());
  expect(result.current.saveState).toBe("error");
  expect(result.current.open).toBe(true);
  await act(() => result.current.save());
  expect(result.current.saveState).toBe("saved");
  expect(f.options.capture).toHaveBeenLastCalledWith(f.instance, "CAPTURE");
  expect(f.options.leave).not.toHaveBeenCalled();
});
it("native sync uses EXPORT and failed exit needs another explicit decision", async () => {
  const f = fixture(true, true);
  f.options.capture.mockResolvedValue(false);
  const { result } = renderHook(() => usePlayerExit(f.options));
  await act(() => result.current.request());
  await act(() => result.current.confirm());
  expect(f.options.capture).toHaveBeenCalledWith(f.instance, "EXPORT");
  expect(f.options.leave).not.toHaveBeenCalled();
  expect(result.current.saveState).toBe("error");
  await act(() => result.current.save());
  expect(f.options.capture).toHaveBeenLastCalledWith(f.instance, "EXPORT");
  await act(() => result.current.confirm());
  expect(f.options.leave).toHaveBeenCalledOnce();
});
it.each(["RUNNING", "PAUSED"] as const)("immersive cancellation waits for neutral then preserves original %s state", async (state) => {
  const f = fixture(true, false, state);
  const { result } = renderHook(() => usePlayerExit(f.options));
  await act(() => result.current.request());
  let cancellation: Promise<void>;
  act(() => { cancellation = result.current.cancel(); });
  expect(result.current.busy).toBe(true);
  act(() => frame({ suspended: false, nowMs: 100, gamepads: [] }));
  act(() => frame({ suspended: false, nowMs: 219, gamepads: [] }));
  expect(f.resume).not.toHaveBeenCalled();
  await act(async () => { frame({ suspended: false, nowMs: 220, gamepads: [] }); await cancellation; });
  expect(f.resume).toHaveBeenCalledTimes(state === "RUNNING" ? 1 : 0);
  expect(result.current.open).toBe(false);
});

it("locks exit decisions while automatic native persistence is in flight", async () => {
  const f = fixture(false, true);
  const { result, rerender } = renderHook((options) => usePlayerExit(options), { initialProps: { ...f.options, saving: true } });
  await act(() => result.current.request());
  expect(result.current.open).toBe(true);
  expect(result.current.busy).toBe(true);
  await act(() => result.current.confirm());
  await act(() => result.current.save());
  await act(() => result.current.cancel());
  expect(f.options.capture).not.toHaveBeenCalled();
  expect(f.options.leave).not.toHaveBeenCalled();
  expect(result.current.saveState).toBe("idle");
  expect(result.current.open).toBe(true);
  vi.spyOn(f.instance, "getCheckpointAvailability").mockReturnValue({ available: false, reason: "UNCHANGED" });
  rerender({ ...f.options, saving: false });
  await act(() => result.current.confirm());
  expect(f.options.leave).toHaveBeenCalledOnce();
  expect(f.options.capture).not.toHaveBeenCalled();
});
it("retries the pending native draft before exit and keeps a failed retry in the menu", async () => {
  const f = fixture(true, true);
  f.options.retry.mockResolvedValueOnce(false).mockResolvedValueOnce(true);
  const { result } = renderHook(() => usePlayerExit({ ...f.options, hasDraft: true }));
  await act(() => result.current.request());
  await act(() => result.current.confirm());
  expect(f.options.retry).toHaveBeenCalledWith(f.instance);
  expect(f.options.capture).not.toHaveBeenCalled();
  expect(f.options.leave).not.toHaveBeenCalled();
  expect(result.current.open).toBe(true);
  await act(() => result.current.save());
  expect(result.current.saveState).toBe("saved");
  expect(f.options.leave).not.toHaveBeenCalled();
});

it.each(["RUNNING", "PAUSED"] as const)("waits for a native export then pauses its restored %s state before opening", async (original) => {
  const f = fixture(true, true, original);
  let finishExport!: () => void;
  const held = new Promise<void>((resolve) => { finishExport = resolve; });
  vi.spyOn(f.instance, "checkpoint").mockImplementation(async () => {
    f.transition("CHECKPOINTING");
    await held;
    f.transition(original);
    return { format: "j2me-rms-bundle-v1-storage-v1", bytes: new Uint8Array([1]), metadata: { revision: "rms" } };
  });
  const exporting = f.instance.checkpoint({ intent: "EXPORT" });
  void exporting;
  const { result } = renderHook(() => usePlayerExit(f.options));
  let opening!: Promise<void>;
  await act(async () => { opening = result.current.request(); });
  expect(f.instance.getState()).toBe("CHECKPOINTING");
  expect(result.current.open).toBe(false);
  expect(result.current.busy).toBe(true);
  expect(f.pause).not.toHaveBeenCalled();
  await act(async () => { finishExport(); await opening; });
  expect(result.current.open).toBe(true);
  expect(f.instance.getState()).toBe("PAUSED");
  expect(f.pause).toHaveBeenCalledTimes(original === "RUNNING" ? 1 : 0);
  let closing!: Promise<void>;
  act(() => { closing = result.current.cancel(); });
  act(() => frame({ suspended: false, nowMs: 100, gamepads: [] }));
  await act(async () => { frame({ suspended: false, nowMs: 220, gamepads: [] }); await closing; });
  expect(f.resume).toHaveBeenCalledTimes(original === "RUNNING" ? 1 : 0);
  expect(f.instance.getState()).toBe(original);
});

it.each(["FAILED", "EXITING", "EXITED"] as const)("releases the checkpoint wait on %s", async (state) => {
  const f = fixture(false, true, "CHECKPOINTING");
  const { result } = renderHook(() => usePlayerExit(f.options));
  let opening!: Promise<void>;
  await act(async () => { opening = result.current.request(); });
  expect(result.current.opening).toBe(true);
  expect(f.listeners.size).toBe(1);
  await act(async () => { f.transition(state); await opening; });
  expect(result.current.busy).toBe(false);
  expect(f.listeners.size).toBe(0);
  expect(f.pause).not.toHaveBeenCalled();
});
it("unsubscribes a pending checkpoint wait on Player unmount", async () => {
  const f = fixture(false, true, "CHECKPOINTING");
  const { result, unmount } = renderHook(() => usePlayerExit(f.options));
  let opening!: Promise<void>;
  await act(async () => { opening = result.current.request(); });
  expect(f.listeners.size).toBe(1);
  await act(async () => { unmount(); await opening; });
  expect(f.listeners.size).toBe(0);
  expect(f.pause).not.toHaveBeenCalled();
});
