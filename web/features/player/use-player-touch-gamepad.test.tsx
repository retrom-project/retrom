import {act, cleanup, renderHook} from "@testing-library/react";
import {afterEach, expect, it, vi} from "vitest";
import type {PlayerRuntimeV1} from "./runtime/contract";
import {readTouchGamepadPreference} from "./touch-gamepad-preference";
import {usePlayerTouchGamepad} from "./use-player-touch-gamepad";

afterEach(() => {cleanup(); localStorage.clear();});
it("retains the toolbar control while a background checkpoint is being captured", () => {
  const runtime = runtimeFor("tic80");
  const hook = renderHook(() => usePlayerTouchGamepad(runtime, "CHECKPOINTING", "tic80", vi.fn()));
  expect(hook.result.current?.enabled).toBe(true);
});
function runtimeFor(coreId: string) {
  let enabled = readTouchGamepadPreference(coreId);
  const controls = {
    getState: () => ({enabled}),
    setEnabled: vi.fn(async (value: boolean) => {enabled = value;}),
  };
  const runtime = {getTouchGamepad: () => controls, subscribe: () => () => {}} as unknown as PlayerRuntimeV1;
  return {current: runtime};
}

it("persists one core's choice across games while keeping another core enabled", async () => {
  const runtime = runtimeFor("tic80");
  const onError = vi.fn();
  const first = renderHook(() => usePlayerTouchGamepad(runtime, "RUNNING", "tic80", onError));
  expect(first.result.current?.enabled).toBe(true);
  await act(async () => {await first.result.current?.toggle();});
  expect(first.result.current?.enabled).toBe(false);
  expect(readTouchGamepadPreference("tic80")).toBe(false);
  first.unmount();
  const second = renderHook(() => usePlayerTouchGamepad(runtimeFor("tic80"), "RUNNING", "tic80", onError));
  expect(second.result.current?.enabled).toBe(false);
  expect(readTouchGamepadPreference("ppsspp")).toBe(true);
  await act(async () => {await second.result.current?.toggle();});
  expect(readTouchGamepadPreference("tic80")).toBe(true);
  expect(onError).not.toHaveBeenCalled();
});

it("does not save a preference when the runtime rejects the change", async () => {
  const runtime = runtimeFor("ppsspp"), onError = vi.fn();
  vi.mocked(runtime.current.getTouchGamepad!()!.setEnabled).mockRejectedValue(new Error("disconnected"));
  const hook = renderHook(() => usePlayerTouchGamepad(runtime, "RUNNING", "ppsspp", onError));
  await act(async () => {await hook.result.current?.toggle();});
  expect(readTouchGamepadPreference("ppsspp")).toBe(true);
  expect(onError).toHaveBeenCalled();
});
