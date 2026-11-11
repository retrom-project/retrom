import {act, renderHook} from "@testing-library/react";
import {afterEach, expect, it} from "vitest";
import type {PlayerRuntimeV1, RuntimeStateV1, RuntimeTouchStickModeV1} from "./runtime/contract";
import {usePlayerTouchStick} from "./use-player-touch-stick";

afterEach(() => localStorage.clear());

function runtimeWith(mode: RuntimeTouchStickModeV1 | null) {
  return {current: {
    subscribe: () => () => undefined,
    getTouchStick: () => mode ? {getState: () => ({mode})} : null,
  } as unknown as PlayerRuntimeV1};
}

it("saves the other mode for the next launch of the core and reports it as pending", () => {
  const runtime = runtimeWith("FLOATING");
  const {result} = renderHook(() => usePlayerTouchStick(runtime, "RUNNING", "fceumm"));
  expect(result.current).toMatchObject({mode: "FLOATING", pending: false});
  act(() => {expect(result.current!.toggle()).toBe(true);});
  expect(result.current).toMatchObject({mode: "FIXED", pending: true});
  expect(localStorage.getItem("retrom:touch-stick:fceumm")).toBe("FIXED");
  act(() => {expect(result.current!.toggle()).toBe(false);});
  expect(result.current).toMatchObject({mode: "FLOATING", pending: false});
});

it("starts from the saved mode and is absent without an on-screen stick or before running", () => {
  localStorage.setItem("retrom:touch-stick:fceumm", "FIXED");
  const {result} = renderHook(() => usePlayerTouchStick(runtimeWith("FIXED"), "RUNNING", "fceumm"));
  expect(result.current).toMatchObject({mode: "FIXED", pending: false});
  for (const [mode, state] of [[null, "RUNNING"], ["FLOATING", "MOUNTING"]] as const) {
    const {result: absent} = renderHook(() => usePlayerTouchStick(runtimeWith(mode), state as RuntimeStateV1, "fceumm"));
    expect(absent.current).toBeNull();
  }
});
