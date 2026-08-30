import { describe, expect, it, vi } from "vitest";

import { handleOnsRuntimeEvent } from "./player-bootstrap-ons";

describe("handleOnsRuntimeEvent", () => {
  it("projects exact ONS content bytes into the Player loading state", () => {
    const target = eventTarget();
    handleOnsRuntimeEvent({
      type: "LOAD_PROGRESS", phase: "PROJECT_CONTENT", loadedBytes: 5, totalBytes: 9,
    }, target);

    expect(target.setLoadProgress).toHaveBeenCalledWith({ loadedBytes: 5, totalBytes: 9 });
    expect(target.setState).not.toHaveBeenCalled();
  });

  it("does not present an unknown total as a percentage", () => {
    const target = eventTarget();
    handleOnsRuntimeEvent({
      type: "LOAD_PROGRESS", phase: "PROJECT_INDEX", loadedBytes: 0, totalBytes: null,
    }, target);

    expect(target.setLoadProgress).not.toHaveBeenCalled();
  });
});

function eventTarget() {
  return {
    setLoadProgress: vi.fn(),
    setMessage: vi.fn(),
    setState: vi.fn(),
  };
}
