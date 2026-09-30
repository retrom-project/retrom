import {act, renderHook} from "@testing-library/react";
import {useState} from "react";
import {afterEach, expect, it, vi} from "vitest";
import {usePlayerRuntimeEffects} from "./player-runtime-effects";
import type {PlayerDebugMetrics} from "./player-debug";
import type {PlayerRuntimeV1} from "./runtime/contract";

afterEach(() => {vi.useRealTimers(); vi.restoreAllMocks();});

it("keeps consecutive frame samples across metric updates and unrelated renders", async () => {
  vi.useFakeTimers({toFake: ["setInterval", "clearInterval", "requestAnimationFrame", "cancelAnimationFrame", "performance"]});
  let frames = 10;
  const stable = {
    state: "running" as const, debugOpen: true, orientationBlocked: false,
    runtime: {current: {getFrameCount: () => frames, getCanvas: () => null} as PlayerRuntimeV1},
    orientationButtonRef: {current: null}, running: {current: false}, pausedRef: {current: false},
    chromePinned: {current: false}, controlsTimer: {current: null},
    clearControlsTimer: vi.fn(), setControlsVisible: vi.fn(), setFullscreen: vi.fn(), setDebugOpen: vi.fn(),
  };
  const hook = renderHook(() => {
    const [metrics, setDebugMetrics] = useState<PlayerDebugMetrics | null>(null);
    usePlayerRuntimeEffects({...stable, setDebugMetrics});
    return metrics;
  });
  await act(() => {vi.advanceTimersByTime(16);});
  hook.rerender();
  frames += 60;
  await act(() => {vi.advanceTimersByTime(984);});
  expect(hook.result.current?.fps).toBeGreaterThan(59);
  expect(hook.result.current?.fps).toBeLessThan(62);
  frames += 60;
  await act(() => {vi.advanceTimersByTime(1_000);});
  expect(hook.result.current?.fps).toBe(60);
  hook.unmount();
  expect(vi.getTimerCount()).toBe(0);
});
