import {act, renderHook} from "@testing-library/react";
import {afterEach, expect, it, vi} from "vitest";
import type {GameSaveSync} from "./game-save-sync";
import {PlayProgressClock} from "./play-progress-clock";
import type {PlayerRuntimeV2} from "./runtime/contract";
import type {RuntimeController} from "./runtime/runtime-controller";
import {useRuntimeSessionAuthority} from "./use-runtime-session-authority";

afterEach(() => {vi.restoreAllMocks(); vi.useRealTimers();});

it("stops producers, uploads, core and progress once without creating an exit save", async () => {
  vi.useFakeTimers();
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, {status: 401}));
  const order: string[] = [];
  const uploads = new AbortController();
  uploads.signal.addEventListener("abort", () => order.push("abort uploads"));
  const save = vi.fn();
  const stop = vi.fn(async () => {order.push("stop saves");});
  const exit = vi.fn(async () => {order.push("exit core");});
  const runtime = {current: {} as PlayerRuntimeV2 | null};
  const controller = {current: {exit, runtime: runtime.current!, signal: new AbortController().signal} as RuntimeController | null};
  const progressClock = new PlayProgressClock();
  progressClock.start(performance.now(), true);
  const progress = vi.fn();
  const params = {
    launchId: "removed", uploads, runtime, controller,
    started: {current: true}, finishing: {current: false}, running: {current: true},
    nativeSave: {current: {stop, save} as unknown as GameSaveSync},
    progressClock: {current: progressClock}, progressTimer: {current: window.setInterval(progress, 30_000)},
    onUnavailable: vi.fn(),
  };
  const {unmount} = renderHook(() => useRuntimeSessionAuthority(params));
  await act(() => vi.advanceTimersByTimeAsync(15_000));
  const elapsed = progressClock.snapshot(performance.now());
  await act(() => vi.advanceTimersByTimeAsync(60_000));
  expect(order).toEqual(["abort uploads", "stop saves", "exit core"]);
  expect(params.finishing.current).toBe(true);
  expect(params.started.current).toBe(false);
  expect(params.running.current).toBe(false);
  expect(runtime.current).toBeNull();
  expect(controller.current).toBeNull();
  expect(params.progressTimer.current).toBeNull();
  expect(progressClock.snapshot(performance.now())).toBe(elapsed);
  expect(params.onUnavailable).toHaveBeenCalledOnce();
  expect(save).not.toHaveBeenCalled();
  expect(progress).not.toHaveBeenCalled();
  unmount();
});
