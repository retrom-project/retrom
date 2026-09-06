import {act, renderHook} from "@testing-library/react";
import {describe, expect, it, vi} from "vitest";
import {GameSaveSync} from "./game-save-sync";
import type {RuntimeController} from "./runtime/runtime-controller";
import {usePlayerRuntimeExit} from "./use-player-runtime-exit";

function fixture() {
  const native = new GameSaveSync({getCheckpointAvailability: () => ({available: false, reason: "UNCHANGED"}),
    checkpoint: vi.fn(), screenshot: vi.fn(), subscribe: () => () => undefined}, vi.fn(), vi.fn());
  const flush = vi.spyOn(native, "flush");
  const stop = vi.spyOn(native, "stop");
  const controllerExit = vi.fn(async () => undefined);
  const controller = {current: {exit: controllerExit} as Pick<RuntimeController, "exit"> as RuntimeController};
  const exit = vi.fn(async () => undefined), strict = vi.fn(async () => undefined), after = vi.fn(async () => undefined);
  const toast = vi.fn();
  const hook = renderHook(() => usePlayerRuntimeExit(controller, {current: native}, exit, strict, after, toast));
  return {...hook, flush, stop, controllerExit, exit, strict, after, toast};
}

describe("native save exit synchronization", () => {
  it("cancels ordinary exit on sync failure so retry remains possible", async () => {
    const f = fixture(); f.flush.mockRejectedValue(new Error("offline"));
    await act(() => f.result.current.exitRuntime());
    expect(f.toast).toHaveBeenCalledWith(expect.stringContaining("退出已取消"), 5000);
    expect(f.stop).not.toHaveBeenCalled(); expect(f.controllerExit).not.toHaveBeenCalled(); expect(f.exit).not.toHaveBeenCalled();
  });
  it("surfaces immersive sync failure without stopping the runtime", async () => {
    const f = fixture(); f.flush.mockRejectedValue(new Error("offline"));
    await expect(f.result.current.exitImmersiveRuntimeStrict()).rejects.toThrow("offline");
    expect(f.stop).not.toHaveBeenCalled(); expect(f.strict).not.toHaveBeenCalled();
  });
  it("drains an already exited provider without requesting another checkpoint", async () => {
    const f = fixture(); f.flush.mockRejectedValue(new Error("already exited"));
    await act(() => f.result.current.exitAfterProviderExit());
    expect(f.flush).not.toHaveBeenCalled(); expect(f.stop).toHaveBeenCalledOnce();
    expect(f.exit).toHaveBeenCalledOnce(); expect(f.after).not.toHaveBeenCalled();
  });
});
