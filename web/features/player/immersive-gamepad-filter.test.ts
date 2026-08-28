import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { gamepadButtonPressed, type GamepadLike } from "./immersive-controls";
import { ImmersiveGamepadFilter, installImmersiveGamepadFilter } from "./immersive-gamepad-filter";

function gamepad(index: number, select = false, start = false): GamepadLike {
  const buttons = Array.from({ length: 16 }, () => ({ pressed: false, touched: false, value: 0 }));
  buttons[8] = { pressed: select, touched: select, value: select ? 1 : 0 };
  buttons[9] = { pressed: start, touched: start, value: start ? 1 : 0 };
  return {
    axes: [0, 0, 0, 0], buttons, connected: true, id: `pad-${index}`,
    index, mapping: "standard", timestamp: 1,
  };
}

function filterSequence(filter: ImmersiveGamepadFilter, times: number[]) {
  filter.filter([gamepad(0, true)], times[0]);
  filter.filter([gamepad(0, true, true)], times[1]);
  filter.filter([gamepad(0)], times[2]);
}

describe("immersive EmulatorJS gamepad filter", () => {
  it("recognizes the menu chord from shell sampling even when the runtime never polls gamepads", () => {
    const onMenuGesture = vi.fn();
    const filter = new ImmersiveGamepadFilter({ activeGamepadIndex: 0, onMenuGesture });

    filter.observe([gamepad(0, true)], 0);
    filter.observe([gamepad(0, true, true)], 40);
    filter.observe([gamepad(0)], 50);
    filter.observe([gamepad(0, true)], 110);
    filter.observe([gamepad(0, true, true)], 150);

    expect(onMenuGesture).toHaveBeenCalledOnce();
  });

  it("delays only the active pad reserved buttons and leaves other pads untouched", () => {
    const other = gamepad(1, true, true);
    const filter = new ImmersiveGamepadFilter({ activeGamepadIndex: 0, onMenuGesture: vi.fn() });
    let result = filter.filter([gamepad(0, true), other], 0);
    expect(gamepadButtonPressed(result[0], 8)).toBe(false);
    expect(result[1]).toBe(other);
    result = filter.filter([gamepad(0, true), other], 100);
    expect(gamepadButtonPressed(result[0], 8)).toBe(true);
  });

  it("suppresses the first chord and returns all-zero input on the second", () => {
    const onMenuGesture = vi.fn();
    const filter = new ImmersiveGamepadFilter({ activeGamepadIndex: 0, onMenuGesture });
    filterSequence(filter, [0, 40, 50]);
    filter.filter([gamepad(0, true), gamepad(1, true)], 110);
    const result = filter.filter([gamepad(0, true, true), gamepad(1, true, true)], 150);
    expect(onMenuGesture).toHaveBeenCalledTimes(1);
    expect(result.flatMap((pad) => pad?.buttons ?? []).every((button) => !button.pressed && button.value === 0)).toBe(true);
    expect(result.flatMap((pad) => pad?.axes ?? []).every((axis) => axis === 0)).toBe(true);
  });

  it("never lets a non-active pad open the menu", () => {
    const onMenuGesture = vi.fn();
    const filter = new ImmersiveGamepadFilter({ activeGamepadIndex: 0, onMenuGesture });
    filterSequence(filter, [0, 40, 50]);
    filter.filter([gamepad(0), gamepad(1, true)], 110);
    filter.filter([gamepad(0), gamepad(1, true, true)], 150);
    expect(onMenuGesture).not.toHaveBeenCalled();
  });
});

describe("iframe navigator filter lifecycle", () => {
  let previous: PropertyDescriptor | undefined;

  beforeEach(() => {
    previous = Object.getOwnPropertyDescriptor(window.navigator, "getGamepads");
  });

  afterEach(() => {
    if (previous) {Object.defineProperty(window.navigator, "getGamepads", previous);}
    else {Reflect.deleteProperty(window.navigator, "getGamepads");}
  });

  it("installs before runtime polling and restores the native reader on teardown", () => {
    const native = vi.fn(() => [gamepad(0)] as unknown as Gamepad[]);
    Object.defineProperty(window.navigator, "getGamepads", { configurable: true, value: native });
    const filter = new ImmersiveGamepadFilter({ activeGamepadIndex: 0, onMenuGesture: vi.fn() });
    const cleanup = installImmersiveGamepadFilter(window, filter);
    const installed = window.navigator.getGamepads;
    expect(installed).not.toBe(native);
    expect(installed()).toHaveLength(1);
    cleanup();
    expect(window.navigator.getGamepads).toBe(native);
  });

  it("fails closed when the iframe navigator cannot be patched", () => {
    const navigator = {} as Navigator;
    Object.defineProperty(navigator, "getGamepads", {
      configurable: false,
      value: () => [] as Gamepad[],
    });
    const runtimeWindow = { navigator, performance: { now: () => 0 } } as Window;
    const filter = new ImmersiveGamepadFilter({ activeGamepadIndex: 0, onMenuGesture: vi.fn() });
    expect(() => installImmersiveGamepadFilter(runtimeWindow, filter)).toThrow("PLAYER_IMMERSIVE_GAMEPAD_FILTER_UNAVAILABLE");
  });
});
