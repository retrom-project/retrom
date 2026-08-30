import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useRuntimeExitHandler } from "./player-runtime-exit";

describe("useRuntimeExitHandler", () => {
  it("disables saving before leaving a standard Player whose core exited itself", () => {
    const available = { current: true };
    const setAvailable = vi.fn();
    const setSyncText = vi.fn();
    const setSyncTone = vi.fn();
    const exit = vi.fn(async () => undefined);
    const exitImmersive = vi.fn(async () => undefined);
    const { result } = renderHook(() => useRuntimeExitHandler(
      available, setAvailable, setSyncText, setSyncTone, "standard", exit, exitImmersive,
    ));

    act(() => result.current());

    expect(available.current).toBe(false);
    expect(setAvailable).toHaveBeenCalledWith(false);
    expect(setSyncText).toHaveBeenCalledWith("游戏已退出");
    expect(setSyncTone).toHaveBeenCalledWith("warning");
    expect(exit).toHaveBeenCalledOnce();
    expect(exitImmersive).not.toHaveBeenCalled();
  });

  it("uses the immersive return handoff when the core exits itself", () => {
    const exit = vi.fn(async () => undefined);
    const exitImmersive = vi.fn(async () => undefined);
    const { result } = renderHook(() => useRuntimeExitHandler(
      { current: true }, vi.fn(), vi.fn(), vi.fn(), "immersive", exit, exitImmersive,
    ));

    act(() => result.current());

    expect(exitImmersive).toHaveBeenCalledOnce();
    expect(exit).not.toHaveBeenCalled();
  });
});
