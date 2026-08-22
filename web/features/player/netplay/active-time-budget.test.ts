import { describe, expect, it, vi } from "vitest";
import { ActiveTimeBudget } from "./active-time-budget";

class Visibility extends EventTarget {
  visibilityState: DocumentVisibilityState = "visible";
  listeners = 0;
  override addEventListener(type: string, callback: EventListenerOrEventListenerObject | null, options?: AddEventListenerOptions | boolean) {
    if (type === "visibilitychange") {this.listeners += 1;}
    super.addEventListener(type, callback, options);
  }
  override removeEventListener(type: string, callback: EventListenerOrEventListenerObject | null, options?: EventListenerOptions | boolean) {
    if (type === "visibilitychange") {this.listeners -= 1;}
    super.removeEventListener(type, callback, options);
  }
  set(value: DocumentVisibilityState) {
    this.visibilityState = value;
    this.dispatchEvent(new Event("visibilitychange"));
  }
}

describe("ActiveTimeBudget", () => {
  it("expires at the exact cumulative visible-time boundary", async () => {
    vi.useFakeTimers();
    const visibility = new Visibility();
    const budget = new ActiveTimeBudget(5_000, { visibility });
    const result = budget.race(new Promise<void>(() => undefined), "FRAME_TIMEOUT");
    await vi.advanceTimersByTimeAsync(4_999);
    let settled = false;
    void result.finally(() => { settled = true; }).catch(() => undefined);
    await Promise.resolve();
    expect(settled).toBe(false);
    await vi.advanceTimersByTimeAsync(1);
    await expect(result).rejects.toThrow("FRAME_TIMEOUT");
    vi.useRealTimers();
  });

  it("does not consume hidden time and resumes with only the remaining budget", async () => {
    vi.useFakeTimers();
    const visibility = new Visibility();
    const budget = new ActiveTimeBudget(5_000, { visibility });
    const result = budget.race(new Promise<void>(() => undefined), "FRAME_TIMEOUT");
    await vi.advanceTimersByTimeAsync(2_000);
    visibility.set("hidden");
    await vi.advanceTimersByTimeAsync(60_000);
    visibility.set("visible");
    await vi.advanceTimersByTimeAsync(2_999);
    let settled = false;
    void result.finally(() => { settled = true; }).catch(() => undefined);
    await Promise.resolve();
    expect(settled).toBe(false);
    await vi.advanceTimersByTimeAsync(1);
    await expect(result).rejects.toThrow("FRAME_TIMEOUT");
    vi.useRealTimers();
  });

  it("releases its timer and visibility listener after success or cancellation", async () => {
    vi.useFakeTimers();
    const successVisibility = new Visibility();
    const success = new ActiveTimeBudget(5_000, { visibility: successVisibility });
    await expect(success.race(Promise.resolve("done"), "FRAME_TIMEOUT")).resolves.toBe("done");
    expect(successVisibility.listeners).toBe(0);
    expect(vi.getTimerCount()).toBe(0);

    const cancelVisibility = new Visibility();
    const cancelled = new ActiveTimeBudget(5_000, { visibility: cancelVisibility });
    const operation = cancelled.race(new Promise<void>(() => undefined), "FRAME_TIMEOUT");
    const cancellation = expect(operation).rejects.toThrow("ACTIVE_TIME_BUDGET_CANCELLED");
    expect(cancelVisibility.listeners).toBe(1);
    cancelled.cancel();
    await cancellation;
    expect(cancelVisibility.listeners).toBe(0);
    expect(vi.getTimerCount()).toBe(0);
    vi.useRealTimers();
  });
});
