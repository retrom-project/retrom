import {act, cleanup, renderHook} from "@testing-library/react";
import {afterEach, expect, it, vi} from "vitest";
import type {PlayerRuntimeV1, RuntimeStateV1} from "./runtime/contract";
import {usePlayerSurfacePause} from "./use-player-surface-pause";

afterEach(cleanup);
function fixture() {
  let state: RuntimeStateV1 = "RUNNING";
  const pause = vi.fn(async () => {state = "PAUSED";});
  const resume = vi.fn(async () => {state = "RUNNING";});
  const ref = {current: {getState: () => state, getCapabilities: () => ({pause: true}), pause, resume} as unknown as PlayerRuntimeV1 | null};
  const manual = vi.fn(), error = vi.fn();
  return {ref, pause, resume, manual, error, setState: (value: RuntimeStateV1) => {state = value;}};
}

it("keeps repeated menu clicks paused and resumes only on the surface command", async () => {
  const f = fixture();
  const view = renderHook(() => usePlayerSurfacePause(f.ref, false, f.manual, f.error));
  await act(() => view.result.current.pause());
  await act(() => view.result.current.pause());
  expect(f.pause).toHaveBeenCalledOnce();
  expect(f.resume).not.toHaveBeenCalled();
  await act(() => view.result.current.resume());
  expect(f.resume).toHaveBeenCalledOnce();
  expect(f.error).not.toHaveBeenCalled();
});

it("waits for an in-flight native pause before processing a fast surface click", async () => {
  const f = fixture();
  let finish = () => {};
  f.pause.mockImplementationOnce(() => new Promise<void>(resolve => {finish = () => {f.setState("PAUSED"); resolve();};}));
  const view = renderHook(() => usePlayerSurfacePause(f.ref, false, f.manual, f.error));
  const first = view.result.current.pause();
  expect(view.result.current.pause()).toBe(first);
  const resumed = view.result.current.resume();
  expect(f.resume).not.toHaveBeenCalled();
  await act(async () => {await Promise.resolve(); finish(); await resumed;});
  expect(f.pause).toHaveBeenCalledOnce();
  expect(f.resume).toHaveBeenCalledOnce();
});

it("does not resume another runtime or a blocked orientation", async () => {
  const f = fixture();
  const view = renderHook(({blocked}) => usePlayerSurfacePause(f.ref, blocked, f.manual, f.error), {initialProps: {blocked: true}});
  await act(() => view.result.current.pause());
  await act(() => view.result.current.resume());
  expect(f.pause).not.toHaveBeenCalled();
  view.rerender({blocked: false});
  await act(() => view.result.current.pause());
  const resumed = view.result.current.resume();
  f.ref.current = null;
  await act(() => resumed);
  expect(f.resume).not.toHaveBeenCalled();
});

it("applies a new menu pause after an in-flight native resume and coalesces repeated surface clicks", async () => {
  const f = fixture();
  f.setState("PAUSED");
  let finish = () => {};
  f.resume.mockImplementationOnce(() => new Promise<void>(resolve => {finish = () => {f.setState("RUNNING"); resolve();};}));
  const view = renderHook(() => usePlayerSurfacePause(f.ref, false, f.manual, f.error));
  const resumed = view.result.current.resume();
  const repeated = view.result.current.resume();
  // The menu's pause wins even if the core has not acknowledged the previous resume yet.
  const paused = view.result.current.pause();
  await act(async () => {await Promise.resolve(); finish(); await Promise.all([resumed, repeated, paused]);});
  expect(f.resume).toHaveBeenCalledOnce();
  expect(f.pause).toHaveBeenCalledOnce();
  expect(f.ref.current?.getState()).toBe("PAUSED");
});
