import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { GamepadFrame } from "./gamepad-source";
import { setActiveImmersiveGamepadIndex } from "./active-gamepad";
import { useImmersiveNavigation } from "./use-navigation";
const source = vi.hoisted(() => ({ receive: (frame: GamepadFrame) => { void frame; } }));
vi.mock("./gamepad-source", () => ({ browserGamepadSource: { subscribe: (receive: typeof source.receive) => { source.receive = receive; return () => undefined; } } }));
afterEach(() => { cleanup(); setActiveImmersiveGamepadIndex(null); });
it("returns to controller waiting after a connected pad navigates and disconnects", () => {
  const onAction = vi.fn();
  const hook = renderHook(() => useImmersiveNavigation(onAction));
  function frame(nowMs: number, pressed: number[] = [], connected = true) {
    act(() => source.receive({ nowMs, suspended: false, gamepads: [{ axes: [0, 0, 0, 0], buttons: Array.from({ length: 17 }, (_, index) => ({ pressed: pressed.includes(index), value: pressed.includes(index) ? 1 : 0 })), mapping: "standard", connected, index: 0 }] }));
  }
  frame(0); frame(100, [0]);
  expect(hook.result.current.ready).toBe(true);
  frame(200); frame(400); frame(500, [15]);
  expect(onAction).toHaveBeenCalledWith("right");
  frame(600, [], false);
  expect(hook.result.current.controller).toBeNull();
  expect(hook.result.current.ready).toBe(false);
});
it("uses S and Y without consuming Tab focus navigation", () => {
  const onAction = vi.fn();
  renderHook(() => useImmersiveNavigation(onAction));
  const tab = new KeyboardEvent("keydown", { key: "Tab", cancelable: true });
  act(() => { window.dispatchEvent(tab); });
  expect(tab.defaultPrevented).toBe(false);
  act(() => { window.dispatchEvent(new KeyboardEvent("keydown", { key: "s" })); });
  act(() => { window.dispatchEvent(new KeyboardEvent("keydown", { key: "Y" })); });
  expect(onAction.mock.calls).toEqual([["menu"], ["favorite"]]);
});
