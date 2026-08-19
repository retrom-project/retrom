import { afterEach, describe, expect, it, vi } from "vitest";
import { captureBeforePause } from "./pause-screenshot";

afterEach(() => vi.useRealTimers());

describe("capture before pausing the emulator", () => {
  it("pauses on time but retains a screenshot that completes afterward", async () => {
    vi.useFakeTimers();
    let finishCapture: (value: string) => void = () => undefined;
    const capture = new Promise<string>((resolve) => { finishCapture = resolve; });
    const pause = vi.fn();
    const result = captureBeforePause(capture, pause, 750, 5_000);

    await vi.advanceTimersByTimeAsync(750);
    expect(pause).toHaveBeenCalledOnce();
    finishCapture("screenshot");
    await expect(result).resolves.toBe("screenshot");
    expect(pause).toHaveBeenCalledOnce();
  });

  it("pauses as soon as a fast screenshot finishes", async () => {
    const pause = vi.fn();
    await expect(captureBeforePause(Promise.resolve("screenshot"), pause)).resolves.toBe("screenshot");
    expect(pause).toHaveBeenCalledOnce();
  });

  it("returns null after the completion deadline without delaying pause", async () => {
    vi.useFakeTimers();
    const pause = vi.fn();
    const result = captureBeforePause(new Promise<string>(() => undefined), pause, 750, 5_000);

    await vi.advanceTimersByTimeAsync(750);
    expect(pause).toHaveBeenCalledOnce();
    await vi.advanceTimersByTimeAsync(4_250);
    await expect(result).resolves.toBeNull();
  });
});
