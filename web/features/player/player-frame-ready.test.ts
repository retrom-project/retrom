import {afterEach, describe, expect, it, vi} from "vitest";

import {waitForPlayerFrame} from "./player-frame-ready";

describe("player frame readiness", () => {
  afterEach(() => {vi.useRealTimers();});

  it("does not depend on requestAnimationFrame while waiting for React to commit the frame", async () => {
    vi.useFakeTimers();
    const animationFrame = vi.spyOn(window, "requestAnimationFrame")
      .mockImplementation(() => 0);
    const reference: {current: HTMLIFrameElement | null} = {current: null};
    const waiting = waitForPlayerFrame(reference, new AbortController().signal);

    reference.current = document.createElement("iframe");
    await vi.runAllTimersAsync();

    await expect(waiting).resolves.toBe(reference.current);
    expect(animationFrame).not.toHaveBeenCalled();
  });

  it("stops promptly when the bootstrap is aborted", async () => {
    vi.useFakeTimers();
    const controller = new AbortController();
    const waiting = waitForPlayerFrame({current: null}, controller.signal);

    controller.abort();

    await expect(waiting).rejects.toMatchObject({name: "AbortError"});
  });
});
