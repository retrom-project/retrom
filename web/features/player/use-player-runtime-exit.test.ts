import {act, renderHook} from "@testing-library/react";
import {describe, expect, it, vi} from "vitest";
import {GameSaveSync} from "./game-save-sync";
import type {RuntimeController} from "./runtime/runtime-controller";
import {usePlayerRuntimeExit} from "./use-player-runtime-exit";

function fixture(nativeEnabled = true) {
  const native = new GameSaveSync({getCheckpointAvailability: () => ({available: false, reason: "UNCHANGED"}),
    checkpoint: vi.fn(), screenshot: vi.fn(), subscribe: () => () => undefined}, vi.fn(), vi.fn(),
  {put: vi.fn(), remove: vi.fn()});
  const flush = vi.spyOn(native, "flush"), stop = vi.spyOn(native, "stop");
  const controllerExit = vi.fn(async () => undefined), pause = vi.fn(async () => undefined), resume = vi.fn(async () => undefined);
  const controller = {current: {exit: controllerExit, runtime: {pause, resume}} as unknown as RuntimeController};
  const exit = vi.fn(async () => undefined), strict = vi.fn(async () => undefined), after = vi.fn(async () => undefined);
  const toast = vi.fn(), decide = vi.fn(async () => true);
  const hook = renderHook(() => usePlayerRuntimeExit(controller, {current: nativeEnabled ? native : null}, exit, strict, after, toast, decide));
  return {...hook, flush, stop, controllerExit, exit, strict, after, toast, decide, pause, resume};
}

describe("native save exit decisions", () => {
  it.each(["exitRuntime", "exitImmersiveRuntimeStrict"] as const)("keeps %s unchanged for instant checkpoint cores", async (action) => {
    const f = fixture(false);
    await act(async () => {await f.result.current[action]();});
    expect(f.decide).not.toHaveBeenCalled(); expect(f.pause).not.toHaveBeenCalled();
    expect(f.flush).not.toHaveBeenCalled(); expect(f.stop).not.toHaveBeenCalled();
    expect(f.controllerExit).toHaveBeenCalledOnce();
    expect(action === "exitRuntime" ? f.exit : f.strict).toHaveBeenCalledOnce();
  });
  it("pauses and drains local writes before asking, then continues on cancel", async () => {
    const f = fixture(); f.decide.mockResolvedValue(false);
    await act(() => f.result.current.exitRuntime());
    expect(f.pause).toHaveBeenCalledOnce(); expect(f.flush).toHaveBeenCalledOnce();
    expect(f.decide).toHaveBeenCalledOnce(); expect(f.resume).toHaveBeenCalledOnce();
    expect(f.stop).not.toHaveBeenCalled(); expect(f.exit).not.toHaveBeenCalled();
  });
  it("allows deciding to save or discard even after local storage fails", async () => {
    const f = fixture(); f.flush.mockRejectedValue(Error("quota"));
    await act(() => f.result.current.exitRuntime());
    expect(f.decide).toHaveBeenCalledOnce(); expect(f.stop).toHaveBeenCalledOnce(); expect(f.exit).toHaveBeenCalledOnce();
  });
  it("returns immersive cancellation to the menu without ending the session", async () => {
    const f = fixture(); f.decide.mockResolvedValue(false);
    await expect(f.result.current.exitImmersiveRuntimeStrict()).resolves.toBe(false);
    expect(f.stop).not.toHaveBeenCalled(); expect(f.strict).not.toHaveBeenCalled();
  });
  it("preserves the draft after a provider exits, without trying to capture or upload", async () => {
    const f = fixture(); await act(() => f.result.current.exitAfterProviderExit());
    expect(f.flush).not.toHaveBeenCalled(); expect(f.decide).not.toHaveBeenCalled();
    expect(f.stop).toHaveBeenCalledOnce(); expect(f.exit).toHaveBeenCalledOnce();
  });
});
