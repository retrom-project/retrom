import { afterEach, describe, expect, it, vi } from "vitest";
import { prepareBlankFrame } from "./blank-frame";

afterEach(() => vi.useRealTimers());

function pendingFrame() {
  const frame = document.createElement("iframe");
  const target = document.createElement("div");
  const append = vi.spyOn(target, "append").mockImplementation(() => undefined);
  return { frame, target, append };
}

describe("blank runtime frame document", () => {
  it("waits for the navigation load before exposing the frame document", async () => {
    const { frame, target, append } = pendingFrame();
    let settled = false;
    const ready = prepareBlankFrame(
      frame,
      target,
      new AbortController().signal,
    ).then(() => {
      settled = true;
    });
    await Promise.resolve();
    expect(append).toHaveBeenCalledWith(frame);
    expect(frame.src).toBe("about:blank");
    expect(settled).toBe(false);
    frame.dispatchEvent(new Event("load"));
    await ready;
    expect(settled).toBe(true);
  });

  it("rejects cancellation without letting a later load mount the runtime", async () => {
    const { frame, target } = pendingFrame();
    const controller = new AbortController();
    const ready = prepareBlankFrame(frame, target, controller.signal);
    const rejection = expect(ready).rejects.toMatchObject({
      name: "AbortError",
    });
    controller.abort();
    frame.dispatchEvent(new Event("load"));
    await rejection;
  });

  it("bounds navigation that never completes", async () => {
    vi.useFakeTimers();
    const { frame, target } = pendingFrame();
    const ready = prepareBlankFrame(
      frame,
      target,
      new AbortController().signal,
    );
    const rejection = expect(ready).rejects.toThrow(
      "PLAYER_RUNTIME_FRAME_TIMEOUT",
    );
    await vi.advanceTimersByTimeAsync(10_000);
    await rejection;
  });
});
