import { describe, expect, it, vi } from "vitest";
import { installPlayerGamepadFilter } from "./gamepad-filter";

describe("Player gamepad filter", () => {
  it("installs before runtime polling, returns bridge snapshots and restores the browser function", () => {
    const physical = [{ index: 0 } as Gamepad, null];
    const original = vi.fn(() => physical as (Gamepad | null)[]);
    const navigatorValue = { getGamepads: original };
    const runtimeWindow = {
      navigator: navigatorValue,
      performance: { now: () => 42, timeOrigin: 1_000 },
    } as unknown as typeof window;
    const filtered = [{ index: 0, timestamp: 2 } as Gamepad, null];
    const bridge = { filterGamepads: vi.fn(() => filtered) };

    const cleanup = installPlayerGamepadFilter(runtimeWindow, bridge);
    expect(runtimeWindow.navigator.getGamepads()).toEqual(filtered);
    expect(bridge.filterGamepads).toHaveBeenCalledWith(physical, 1_042);
    cleanup();
    expect(runtimeWindow.navigator.getGamepads).toBe(original);
  });

  it("keeps Player usable when the browser exposes no Gamepad API", () => {
    const navigatorValue = {} as Navigator;
    const runtimeWindow = {
      navigator: navigatorValue,
      performance: { now: () => 1, timeOrigin: 1_000 },
    } as unknown as typeof window;
    const bridge = { filterGamepads: vi.fn((pads) => pads) };
    const cleanup = installPlayerGamepadFilter(runtimeWindow, bridge);
    expect(runtimeWindow.navigator.getGamepads()).toEqual([]);
    cleanup();
    expect("getGamepads" in navigatorValue).toBe(false);
  });
});
