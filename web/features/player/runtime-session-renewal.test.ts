import {act, renderHook} from "@testing-library/react";
import {afterEach, describe, expect, it, vi} from "vitest";
import {useRuntimeSessionRenewal} from "./runtime-session-renewal";

const hour = 60 * 60 * 1000;

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe("shared runtime renewal", () => {
  it("renews each running game independently and stops only the closed game", async () => {
    vi.useFakeTimers();
    const request = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, {status: 204}));
    const playing = {current: false};
    const first = renderHook(() => useRuntimeSessionRenewal("first", playing, {current:false}));
    const second = renderHook(() => useRuntimeSessionRenewal("second", {current:true}, {current:false}));
    await act(() => vi.advanceTimersByTimeAsync(hour));
    expect(request.mock.calls.map(([url]) => url)).toEqual(["/runtime/launches/second/renew"]);
    playing.current = true;
    request.mockClear();
    await act(() => vi.advanceTimersByTimeAsync(hour));
    expect(request.mock.calls.map(([url]) => url)).toEqual(["/runtime/launches/first/renew", "/runtime/launches/second/renew"]);
    first.unmount();
    request.mockClear();
    await act(() => vi.advanceTimersByTimeAsync(hour));
    expect(request.mock.calls.map(([url]) => url)).toEqual(["/runtime/launches/second/renew"]);
    second.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("retries network failure on reconnect without accumulating concurrent requests", async () => {
    vi.useFakeTimers();
    let complete!: (response: Response) => void;
    const request = vi.spyOn(globalThis, "fetch").mockRejectedValueOnce(new Error("offline"))
      .mockImplementationOnce(() => new Promise((resolve) => {complete = resolve;}));
    const {unmount} = renderHook(() => useRuntimeSessionRenewal("game", {current:true}, {current:false}));
    await act(() => vi.advanceTimersByTimeAsync(hour));
    await act(async () => {window.dispatchEvent(new Event("online"));});
    await act(() => vi.advanceTimersByTimeAsync(hour));
    expect(request).toHaveBeenCalledTimes(2);
    const options = request.mock.calls[1]?.[1];
    expect(options).toMatchObject({method: "POST", credentials: "same-origin", cache: "no-store"});
    unmount();
    expect(options?.signal?.aborted).toBe(true);
    await act(async () => {complete(new Response(null, {status: 204}));});
    window.dispatchEvent(new Event("online"));
    document.dispatchEvent(new Event("visibilitychange"));
    expect(request).toHaveBeenCalledTimes(2);
  });
});
