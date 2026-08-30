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
    expect(target.onExitRequested).toHaveBeenCalledOnce();
  });
});

function eventTarget() {
  return {
    setLoadProgress: vi.fn(),
    setManualSaveAvailable: vi.fn(),
    setMessage: vi.fn(),
    setState: vi.fn(),
    onExitRequested: vi.fn(),
  };
}
