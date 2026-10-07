import { act, cleanup, fireEvent, render, renderHook } from "@testing-library/react";
import { useRef } from "react";
import { useModalFocus } from "@/components/modal-focus";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { GamepadFrameListener } from "@/features/immersive/gamepad-source";
import { browserGamepadSource } from "@/features/immersive/gamepad-source";
import { setActiveImmersiveGamepadIndex } from "@/features/immersive/active-gamepad";
import { runtimeFixture } from "./player-test-fixture";
import { usePlayerControls } from "./use-player-controls";

vi.mock("@/features/immersive/gamepad-source", () => ({
  browserGamepadSource: { subscribe: vi.fn() },
}));
afterEach(cleanup);
let frameListener: GamepadFrameListener;
beforeEach(() => {
  vi.clearAllMocks();
  setActiveImmersiveGamepadIndex(null);
  vi.mocked(browserGamepadSource.subscribe).mockImplementation((listener) => {
    frameListener = listener;
    return () => undefined;
  });
});

it("claims a direct-player controller and keeps held menu edges across renders", () => {
  const instance = runtimeFixture();
  const capabilities = instance.getCapabilities();
  vi.spyOn(instance, "getCapabilities").mockReturnValue({
    ...capabilities,
    inputFilter: true,
  });
  vi.spyOn(instance, "getInputCapabilities").mockReturnValue({
    hostShortcuts: ["MENU"],
  });
  const filter = vi.spyOn(instance, "setInputFilter");
  const menu = vi.fn();
  const runtime = { current: instance };
  const { rerender } = renderHook(
    ({ open }) =>
      usePlayerControls(
        runtime,
        menu,
        () => undefined,
        { suppressInput: open, menuOpen: open, immersive: false, dialogOpen: false, onCancel: vi.fn(), onFailure: vi.fn() },
      ),
    { initialProps: { open: false } },
  );
  act(() => frameListener(frame(true, 0)));
  expect(menu).toHaveBeenCalledOnce();
  expect(filter).toHaveBeenLastCalledWith({
    activeGamepadIndex: 2,
    suppressInput: false,
  });
  rerender({ open: true });
  act(() => frameListener(frame(true, 16)));
  expect(menu).toHaveBeenCalledOnce();
  expect(browserGamepadSource.subscribe).toHaveBeenCalledOnce();
  expect(filter).toHaveBeenLastCalledWith({
    activeGamepadIndex: 2,
    suppressInput: true,
  });
  act(() => frameListener(frame(false, 32)));
  act(() => frameListener(frame(false, 180)));
  act(() => frameListener(frame(true, 196)));
  expect(menu).toHaveBeenCalledTimes(2);
});

function frame(menuPressed: boolean, nowMs: number) {
  return {
    nowMs,
    suspended: false,
    gamepads: [
      {
        index: 2,
        connected: true,
        mapping: "standard",
        axes: [0, 0],
        buttons: Array.from({ length: 16 }, (_, index) => ({
          pressed: index === 8 && menuPressed,
          value: index === 8 && menuPressed ? 1 : 0,
        })),
      },
    ],
  };
}

it("does not toggle the background menu when an exit dialog owns Escape", () => {
  const instance = runtimeFixture();
  vi.spyOn(instance, "getInputCapabilities").mockReturnValue({ hostShortcuts: ["MENU"] });
  const menu = vi.fn();
  renderHook(() => usePlayerControls({ current: instance }, menu, vi.fn(), { suppressInput: true, menuOpen: false, immersive: false, dialogOpen: true, onCancel: vi.fn(), onFailure: vi.fn() }));
  fireEvent.keyDown(window, { key: "Escape" });
  expect(menu).not.toHaveBeenCalled();
});
it("reserves M and a repeated Select+Start chord for the immersive game menu", () => {
  const instance = runtimeFixture();
  vi.spyOn(instance, "getInputCapabilities").mockReturnValue({ hostShortcuts: ["MENU"] });
  const menu = vi.fn();
  renderHook(() => usePlayerControls({ current: instance }, menu, vi.fn(), { suppressInput: false, menuOpen: false, immersive: true, dialogOpen: false, onCancel: vi.fn(), onFailure: vi.fn() }));
  fireEvent.keyDown(window, { key: "Escape" });
  expect(menu).not.toHaveBeenCalled();
  fireEvent.keyDown(window, { key: "m" });
  expect(menu).toHaveBeenCalledOnce();
  function chord(pressed: boolean, now: number) { const value = frame(pressed, now); value.gamepads[0].buttons[9] = { pressed, value: pressed ? 1 : 0 }; act(() => frameListener(value)); }
  chord(true, 0); chord(false, 120); chord(true, 240);
  expect(menu).toHaveBeenCalledTimes(2);
});

it("sends B to the active nested editor dialog without closing the whole player overlay", () => {
  const instance = runtimeFixture();
  const outerCancel = vi.fn(), innerCancel = vi.fn();
  function NestedEditor() {
    const panel = useRef<HTMLDivElement>(null);
    useModalFocus({ open: true, locked: false, panel, onCancel: innerCancel });
    return <section role="dialog" aria-label="游戏修改"><div ref={panel} role="dialog" aria-label="选择地图"><input aria-label="查找地图" /></div></section>;
  }
  render(<NestedEditor />);
  setActiveImmersiveGamepadIndex(2);
  renderHook(() => usePlayerControls({ current: instance }, vi.fn(), vi.fn(), { suppressInput: true, menuOpen: false, immersive: true, dialogOpen: true, onCancel: outerCancel, onFailure: vi.fn() }));
  act(() => frameListener(frame(false, 0)));
  act(() => frameListener(frame(false, 120)));
  const cancel = frame(false, 121);
  cancel.gamepads[0].buttons[1] = { pressed: true, value: 1 };
  act(() => frameListener(cancel));
  expect(innerCancel).toHaveBeenCalledOnce();
  expect(outerCancel).not.toHaveBeenCalled();
});
