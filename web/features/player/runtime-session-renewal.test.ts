import {act, renderHook} from "@testing-library/react";
import {afterEach, describe, expect, it, vi} from "vitest";
import {useRuntimeSessionRenewal} from "./runtime-session-renewal";

const interval = 15_000;

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe("shared runtime renewal", () => {
  it("renews each running game independently and stops only the closed game", async () => {
    vi.useFakeTimers();
    const request = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, {status: 204}));
    const playing = {current: false};
    const first = renderHook(() => useRuntimeSessionRenewal("first", playing, {current:false}, vi.fn()));
    const second = renderHook(() => useRuntimeSessionRenewal("second", {current:true}, {current:false}, vi.fn()));
    await act(() => vi.advanceTimersByTimeAsync(interval));
    expect(request.mock.calls.map(([url]) => url)).toEqual(["/runtime/launches/second/renew"]);
    playing.current = true;
    request.mockClear();
    await act(() => vi.advanceTimersByTimeAsync(interval));
    expect(request.mock.calls.map(([url]) => url)).toEqual(["/runtime/launches/first/renew", "/runtime/launches/second/renew"]);
    first.unmount();
    request.mockClear();
    await act(() => vi.advanceTimersByTimeAsync(interval));
    expect(request.mock.calls.map(([url]) => url)).toEqual(["/runtime/launches/second/renew"]);
    second.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("retries network failure on reconnect without accumulating concurrent requests", async () => {
    vi.useFakeTimers();
    let complete!: (response: Response) => void;
    const request = vi.spyOn(globalThis, "fetch").mockRejectedValueOnce(new Error("offline"))
      .mockImplementationOnce(() => new Promise((resolve) => {complete = resolve;}));
    const {unmount} = renderHook(() => useRuntimeSessionRenewal("game", {current:true}, {current:false}, vi.fn()));
    await act(() => vi.advanceTimersByTimeAsync(interval));
    await act(async () => {window.dispatchEvent(new Event("online"));});
    await act(() => vi.advanceTimersByTimeAsync(interval));
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


describe("runtime authority", () => {
  it("stops once on rejection, including a paused but started game", async () => {
    vi.useFakeTimers();
    const unavailable = vi.fn();
    const request = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, {status: 401}));
    const {unmount} = renderHook(() => useRuntimeSessionRenewal("revoked", {current:true}, {current:false}, unavailable));
    await act(() => vi.advanceTimersByTimeAsync(interval * 3));
    await act(async () => {window.dispatchEvent(new Event("online"));});
    expect(unavailable).toHaveBeenCalledOnce();
    expect(request).toHaveBeenCalledOnce();
    unmount();
  });

  it("keeps running through offline and temporary server failure, then checks on reconnect", async () => {
    vi.useFakeTimers();
    const unavailable = vi.fn();
    vi.spyOn(globalThis, "fetch").mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce(new Response(null, {status: 503}))
      .mockResolvedValueOnce(new Response(null, {status: 401}));
    const {unmount} = renderHook(() => useRuntimeSessionRenewal("game", {current:true}, {current:false}, unavailable));
    await act(() => vi.advanceTimersByTimeAsync(interval * 2));
    expect(unavailable).not.toHaveBeenCalled();
    await act(async () => {window.dispatchEvent(new Event("online"));});
    expect(unavailable).toHaveBeenCalledOnce();
    unmount();
  });

  it("ignores rejection from the previous launch after a route change", async () => {
    vi.useFakeTimers();
    let complete!: (response: Response) => void;
    const unavailable = vi.fn();
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(resolve => {complete = resolve;}));
    const started = {current:true};
    const finishing = {current:false};
    const {rerender, unmount} = renderHook(({id}) => useRuntimeSessionRenewal(id, started, finishing, unavailable), {initialProps: {id: "old"}});
    await act(() => vi.advanceTimersByTimeAsync(interval));
    rerender({id: "new"});
    await act(async () => {complete(new Response(null, {status: 401}));});
    expect(unavailable).not.toHaveBeenCalled();
    unmount();
  });
});
