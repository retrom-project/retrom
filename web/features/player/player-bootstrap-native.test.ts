import { afterEach, describe, expect, it, vi } from "vitest";
import { ImmersiveGamepadFilter } from "./immersive-gamepad-filter";
import type { GamepadLike } from "./immersive-controls";
import { schedulePlayerCanvasRefresh } from "./player-bootstrap";
import { installRuntimeImmersiveGamepadFilter } from "./runtime-immersive-gamepad";

function gamepad(select = false, start = false): GamepadLike {
  const buttons = Array.from({ length: 16 }, () => ({ pressed: false, touched: false, value: 0 }));
  buttons[8] = { pressed: select, touched: select, value: select ? 1 : 0 };
  buttons[9] = { pressed: start, touched: start, value: start ? 1 : 0 };
  return { axes: [0, 0], buttons, connected: true, id: "pad-0", index: 0, mapping: "standard", timestamp: 1 };
}

afterEach(() => {document.body.replaceChildren();});

function runtimeFrameWindow() {
  const frame = document.createElement("iframe");
  document.body.append(frame);
  return frame.contentWindow!;
}

describe("native RPG Maker frame refresh", () => {
  it("never reads requestAnimationFrame from the unique-origin runtime Window", () => {
    const refresh = vi.fn();
    const parentFrame = vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      callback(0);
      return 1;
    });
    const uniqueOriginWindow = {
      get requestAnimationFrame(): never {
        throw new DOMException("Blocked a frame with origin", "SecurityError");
      },
    } as unknown as Window;

    expect(() => schedulePlayerCanvasRefresh(true, uniqueOriginWindow, refresh)).not.toThrow();
    expect(parentFrame).toHaveBeenCalledOnce();
    expect(refresh).toHaveBeenCalledOnce();
  });

  it("installs the immersive Select+Start filter for non-EmulatorJS runtime windows", () => {
    let current = gamepad();
    const runtimeWindow = runtimeFrameWindow();
    Object.defineProperty(runtimeWindow.navigator, "getGamepads", {
      configurable: true,
      value: () => [current] as unknown as Gamepad[],
    });
    const onMenuGesture = vi.fn();
    const filter = new ImmersiveGamepadFilter({ activeGamepadIndex: 0, onMenuGesture });
    const now = vi.spyOn(runtimeWindow.performance, "now");
    for (const value of [0, 40, 50, 110, 150]) {now.mockReturnValueOnce(value);}

    const cleanup = installRuntimeImmersiveGamepadFilter("immersive", runtimeWindow, filter);
    current = gamepad(true);
    runtimeWindow.navigator.getGamepads();
    current = gamepad(true, true);
    runtimeWindow.navigator.getGamepads();
    current = gamepad();
    runtimeWindow.navigator.getGamepads();
    current = gamepad(true);
    runtimeWindow.navigator.getGamepads();
    current = gamepad(true, true);
    runtimeWindow.navigator.getGamepads();

    expect(onMenuGesture).toHaveBeenCalledOnce();
    cleanup?.();
    now.mockRestore();
  });

  it("does not patch standard Player runtime windows", () => {
    const runtimeWindow = runtimeFrameWindow();
    const nativeGetGamepads = vi.fn(() => [] as Gamepad[]);
    Object.defineProperty(runtimeWindow.navigator, "getGamepads", { configurable: true, value: nativeGetGamepads });
    const filter = new ImmersiveGamepadFilter({ activeGamepadIndex: 0, onMenuGesture: vi.fn() });

    expect(installRuntimeImmersiveGamepadFilter("standard", runtimeWindow, filter)).toBeUndefined();
    expect(runtimeWindow.navigator.getGamepads).toBe(nativeGetGamepads);
  });
});
