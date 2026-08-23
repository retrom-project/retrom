import { describe, expect, it, vi } from "vitest";
import type { ControllerSnapshot } from "@/features/gamepad/types";
import {
  PlayerGamepadHostControls,
  releaseAllPlayerInputs,
} from "./gamepad-host-controls";

function snapshot(pressed: number[] = [], timestamp = 1, index = 0): ControllerSnapshot {
  const active = new Set(pressed);
  return {
    index,
    connected: true,
    mapping: "standard",
    timestamp,
    buttons: Array.from({ length: 17 }, (_, button) => ({
      pressed: active.has(button),
      touched: active.has(button),
      value: active.has(button) ? 1 : 0,
    })),
    axes: [0, 0, 0, 0],
  };
}

function gamepad(pressed: number[] = [], timestamp = 1, index = 0) {
  const value = snapshot(pressed, timestamp, index);
  return {
    id: "not-recorded",
    index,
    connected: true,
    mapping: "standard",
    timestamp,
    buttons: value.buttons,
    axes: value.axes,
  } as Gamepad;
}

function readyOverlay(host: PlayerGamepadHostControls) {
  host.openOverlay();
  host.sample([snapshot([], 2)], 0);
  host.sample([snapshot([], 3)], 120);
  host.drainActions();
}

describe("Player gamepad host controls", () => {
  it("always removes the center button and emits short/long system actions once", () => {
    const host = new PlayerGamepadHostControls(0);
    const filtered = host.filterGamepads([gamepad([16], 1)], 0)[0]!;
    expect(filtered.buttons[16]).toEqual({ pressed: false, touched: false, value: 0 });
    expect(host.drainActions()).toEqual([{ type: "open-menu" }]);
    host.filterGamepads([gamepad([16], 2)], 1_200);
    expect(host.drainActions()).toEqual([{ type: "open-exit-confirmation" }]);
    host.filterGamepads([gamepad([16], 3)], 1_500);
    expect(host.drainActions()).toEqual([]);
  });

  it("suppresses Select plus Start inside 100ms and never exposes the combination", () => {
    const host = new PlayerGamepadHostControls(0);
    let filtered = host.filterGamepads([gamepad([8], 1)], 0)[0]!;
    expect(filtered.buttons[8]?.value).toBe(0);
    filtered = host.filterGamepads([gamepad([8, 9], 2)], 90)[0]!;
    expect(filtered.buttons[8]?.value).toBe(0);
    expect(filtered.buttons[9]?.value).toBe(0);
    expect(host.drainActions()).toEqual([{ type: "open-menu" }]);
  });

  it("keeps tracking a held shortcut after its menu takes input ownership", () => {
    const center = new PlayerGamepadHostControls(0);
    center.filterGamepads([gamepad([16], 1)], 0);
    expect(center.drainActions()).toEqual([{ type: "open-menu" }]);
    center.openOverlay();
    center.sample([snapshot([16], 2)], 1_200);
    expect(center.drainActions()).toEqual([{ type: "open-exit-confirmation" }]);

    const combination = new PlayerGamepadHostControls(0);
    combination.filterGamepads([gamepad([8], 1)], 0);
    combination.filterGamepads([gamepad([8, 9], 2)], 90);
    expect(combination.drainActions()).toEqual([{ type: "open-menu" }]);
    combination.openOverlay();
    combination.sample([snapshot([8, 9], 3)], 1_290);
    expect(combination.drainActions()).toEqual([{ type: "open-exit-confirmation" }]);
  });

  it("returns a held single key after 100ms and converts a fast tap to one 50ms pulse", () => {
    const held = new PlayerGamepadHostControls(0);
    expect(held.filterGamepads([gamepad([9], 1)], 0)[0]!.buttons[9]?.value).toBe(0);
    expect(held.filterGamepads([gamepad([9], 2)], 101)[0]!.buttons[9]?.value).toBe(1);

    const tapped = new PlayerGamepadHostControls(0);
    tapped.filterGamepads([gamepad([8], 1)], 0);
    expect(tapped.filterGamepads([gamepad([], 2)], 40)[0]!.buttons[8]?.value).toBe(1);
    expect(tapped.filterGamepads([gamepad([], 3)], 89)[0]!.buttons[8]?.value).toBe(1);
    expect(tapped.filterGamepads([gamepad([], 4)], 91)[0]!.buttons[8]?.value).toBe(0);
  });

  it("zeros every local controller while overlays own input and waits for neutral before gameplay", () => {
    const host = new PlayerGamepadHostControls(0);
    readyOverlay(host);
    const filtered = host.filterGamepads([gamepad([0, 12], 4)], 140)[0]!;
    expect(filtered.buttons.every((button) => button.value === 0)).toBe(true);
    expect(filtered.axes.every((axis) => axis === 0)).toBe(true);
    expect(host.drainActions()).toEqual([{ type: "confirm" }]);
    host.requestGameplay();
    host.sample([snapshot([], 5)], 200);
    host.sample([snapshot([], 6)], 320);
    expect(host.drainActions()).toEqual([{ type: "gameplay-ready" }]);
    expect(host.inputOwner()).toBe("gameplay");
  });

  it("requires a fresh neutral gate after a hidden or unfocused gameplay surface", () => {
    const host = new PlayerGamepadHostControls(0);
    host.suspendUntilNeutral();
    expect(host.inputOwner()).toBe("closing");
    host.sample([snapshot([0], 2)], 0);
    host.sample([snapshot([], 3)], 100);
    host.sample([snapshot([], 4)], 219);
    expect(host.drainActions()).toEqual([]);
    host.sample([snapshot([], 5)], 220);
    expect(host.drainActions()).toEqual([{ type: "gameplay-ready" }]);
    expect(host.inputOwner()).toBe("gameplay");
  });

  it("claims replacement controllers anonymously and reports disconnect", () => {
    const host = new PlayerGamepadHostControls(0);
    host.sample([], 0);
    expect(host.drainActions()).toEqual([{ type: "disconnected", index: 0 }]);
    host.sample([null, snapshot([0], 2, 1)], 10);
    expect(host.drainActions()).toEqual([{ type: "claimed", index: 1, centerButtonObservable: true }]);
  });

  it("releases all local controls before pausing or opening a menu", () => {
    const simulateInput = vi.fn();
    expect(releaseAllPlayerInputs({ gameManager: { simulateInput }, on: () => undefined })).toBe(true);
    expect(simulateInput).toHaveBeenCalledTimes(96);
    expect(simulateInput).toHaveBeenCalledWith(0, 0, 0);
    expect(simulateInput).toHaveBeenCalledWith(3, 23, 0);
  });
});
