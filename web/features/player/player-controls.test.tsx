import { act, renderHook } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type { GamepadFrameListener } from "@/features/immersive/gamepad-source";
import { browserGamepadSource } from "@/features/immersive/gamepad-source";
import { setActiveImmersiveGamepadIndex } from "@/features/immersive/active-gamepad";
import { runtimeFixture } from "./player-test-fixture";
import { usePlayerControls } from "./use-player-controls";

vi.mock("@/features/immersive/gamepad-source", () => ({
  browserGamepadSource: { subscribe: vi.fn() },
}));
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
        open,
        () => undefined,
        open,
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
