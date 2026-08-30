import { describe, expect, it, vi } from "vitest";

import { handleRetromRuntimeEvent } from "./player-bootstrap-ons";

describe("handleRetromRuntimeEvent", () => {
  it("projects exact ONS content bytes into the Player loading state", () => {
    const target = eventTarget();
    handleRetromRuntimeEvent({
      type: "LOAD_PROGRESS", phase: "PROJECT_CONTENT", loadedBytes: 5, totalBytes: 9,
    }, target);

    expect(target.setLoadProgress).toHaveBeenCalledWith({ loadedBytes: 5, totalBytes: 9 });
    expect(target.setState).not.toHaveBeenCalled();
  });

  it("does not present an unknown total as a percentage", () => {
    const target = eventTarget();
    handleRetromRuntimeEvent({
      type: "LOAD_PROGRESS", phase: "PROJECT_INDEX", loadedBytes: 0, totalBytes: null,
    }, target);

    expect(target.setLoadProgress).not.toHaveBeenCalled();
  });

  it("disables saving and asks the Player to close when the core exits itself", () => {
    const target = eventTarget();

    handleRetromRuntimeEvent({ type: "EXIT_REQUESTED" }, target);

    expect(target.setManualSaveAvailable).toHaveBeenCalledWith(false);
    expect(target.manualSaveAvailableRef.current).toBe(false);
    expect(target.onExitRequested).toHaveBeenCalledOnce();
  });

  it("tracks a runtime becoming checkpointable after its initial busy state", () => {
    const target = eventTarget();

    handleRetromRuntimeEvent({
      type: "CHECKPOINT_AVAILABILITY_CHANGED", availability: { available: false, blocker: "BUSY" },
    }, target);
    expect(target.manualSaveAvailableRef.current).toBe(false);
    expect(target.setManualSaveAvailable).toHaveBeenLastCalledWith(false);
    expect(target.setSyncText).toHaveBeenLastCalledWith("当前场景暂不可存档");
    expect(target.setSyncTone).toHaveBeenLastCalledWith("busy");

    handleRetromRuntimeEvent({
      type: "CHECKPOINT_AVAILABILITY_CHANGED", availability: { available: true, blocker: null },
    }, target);
    expect(target.manualSaveAvailableRef.current).toBe(true);
    expect(target.setManualSaveAvailable).toHaveBeenLastCalledWith(true);
    expect(target.setSyncText).toHaveBeenLastCalledWith("可创建存档");
    expect(target.setSyncTone).toHaveBeenLastCalledWith("synced");

    handleRetromRuntimeEvent({
      type: "CHECKPOINT_AVAILABILITY_CHANGED", availability: { available: false, blocker: "UNSUPPORTED" },
    }, target);
    expect(target.setSyncText).toHaveBeenLastCalledWith("当前状态含暂不支持的存档数据");
    expect(target.setSyncTone).toHaveBeenLastCalledWith("warning");
  });
});

function eventTarget() {
  return {
    manualSaveAvailableRef: { current: true },
    setLoadProgress: vi.fn(),
    setManualSaveAvailable: vi.fn(),
    setMessage: vi.fn(),
    setState: vi.fn(),
    setSyncText: vi.fn(),
    setSyncTone: vi.fn(),
    onExitRequested: vi.fn(),
  };
}
