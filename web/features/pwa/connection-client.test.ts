import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { observeConnection } from "./connection-client";

let stop: (() => void) | undefined;
const browser = { onLine: false };

beforeEach(() => {
  vi.useFakeTimers();
  browser.onLine = false;
  vi.stubGlobal("navigator", browser);
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
});
afterEach(() => {
  stop?.(); stop = undefined;
  vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals();
});

test("retries a real outage and stops probing when the server responds despite a stale browser flag", async () => {
  const fetcher = vi.fn().mockRejectedValueOnce(new TypeError("Offline")).mockResolvedValue(new Response());
  vi.stubGlobal("fetch", fetcher);
  const changed = vi.fn();
  stop = observeConnection(changed);
  await vi.advanceTimersByTimeAsync(0);
  expect(changed.mock.calls).toEqual([[false]]);
  await vi.advanceTimersByTimeAsync(5_000);
  expect(changed.mock.calls).toEqual([[false], [true]]);
  expect(browser.onLine).toBe(false);
  expect(fetcher).toHaveBeenCalledWith("/health/ready", { cache: "no-store", signal: expect.any(AbortSignal) });
  await vi.advanceTimersByTimeAsync(30_000);
  expect(fetcher).toHaveBeenCalledTimes(2);
});

test("a server readiness error still proves the network connection is available", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 503 })));
  const changed = vi.fn();
  stop = observeConnection(changed);
  await vi.advanceTimersByTimeAsync(0);
  expect(changed.mock.calls).toEqual([[false], [true]]);
});

test("recovers a missed online event during an offline retry without making online background requests", async () => {
  const fetcher = vi.fn().mockRejectedValue(new TypeError("Offline"));
  vi.stubGlobal("fetch", fetcher);
  const changed = vi.fn();
  stop = observeConnection(changed);
  await vi.advanceTimersByTimeAsync(0);
  browser.onLine = true;
  await vi.advanceTimersByTimeAsync(5_000);
  expect(changed.mock.calls).toEqual([[false], [true]]);
  expect(fetcher).toHaveBeenCalledTimes(1);
  await vi.advanceTimersByTimeAsync(30_000);
  expect(fetcher).toHaveBeenCalledTimes(1);
});

test("coalesces foreground events, pauses hidden probes and ignores a response after cancellation", async () => {
  let respond!: (response: Response) => void;
  const fetcher = vi.fn().mockReturnValueOnce(new Promise<Response>(resolve => { respond = resolve; }))
    .mockResolvedValue(new Response());
  vi.stubGlobal("fetch", fetcher);
  const changed = vi.fn();
  stop = observeConnection(changed);
  await vi.advanceTimersByTimeAsync(0);
  window.dispatchEvent(new Event("focus"));
  window.dispatchEvent(new Event("pageshow"));
  expect(fetcher).toHaveBeenCalledTimes(1);
  const signal = fetcher.mock.calls[0][1].signal as AbortSignal;
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
  document.dispatchEvent(new Event("visibilitychange"));
  expect(signal.aborted).toBe(true);
  respond(new Response());
  await vi.advanceTimersByTimeAsync(30_000);
  expect(changed.mock.calls).toEqual([[false]]);
  expect(fetcher).toHaveBeenCalledTimes(1);
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
  document.dispatchEvent(new Event("visibilitychange"));
  await vi.advanceTimersByTimeAsync(0);
  expect(changed.mock.calls).toEqual([[false], [true]]);
});

test("a previous probe cannot clear a newer offline state", async () => {
  let respond!: (response: Response) => void;
  const fetcher = vi.fn().mockReturnValueOnce(new Promise<Response>(resolve => { respond = resolve; }))
    .mockRejectedValue(new TypeError("Offline"));
  vi.stubGlobal("fetch", fetcher);
  const changed = vi.fn();
  stop = observeConnection(changed);
  await vi.advanceTimersByTimeAsync(0);
  browser.onLine = true; window.dispatchEvent(new Event("online"));
  browser.onLine = false; window.dispatchEvent(new Event("offline"));
  respond(new Response());
  await vi.advanceTimersByTimeAsync(0);
  expect(changed.mock.calls).toEqual([[false], [true], [false]]);
});

test("times out stuck probes and cancels all retry work on disposal", async () => {
  const fetcher = vi.fn((_url: string, { signal }: { signal: AbortSignal }) => new Promise<Response>((_resolve, reject) => {
    signal.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true });
  }));
  vi.stubGlobal("fetch", fetcher);
  const changed = vi.fn();
  stop = observeConnection(changed);
  await vi.advanceTimersByTimeAsync(3_000);
  expect(fetcher.mock.calls[0][1].signal.aborted).toBe(true);
  await vi.advanceTimersByTimeAsync(5_000);
  expect(fetcher).toHaveBeenCalledTimes(2);
  stop();
  expect(fetcher.mock.calls[1][1].signal.aborted).toBe(true);
  await vi.advanceTimersByTimeAsync(30_000);
  window.dispatchEvent(new Event("focus"));
  window.dispatchEvent(new Event("offline"));
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(changed.mock.calls).toEqual([[false]]);
});
