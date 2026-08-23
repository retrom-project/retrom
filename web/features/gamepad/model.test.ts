import { describe, expect, it } from "vitest";
import {
  initialNavigationControllerState,
  isStandardNavigationController,
  suspendNavigationController,
  updateNavigationController,
} from "./model";
import type { ControllerSnapshot } from "./types";

function pad(options: {
  index?: number;
  mapping?: string;
  pressed?: number[];
  axes?: number[];
  connected?: boolean;
} = {}): ControllerSnapshot {
  const pressed = new Set(options.pressed ?? []);
  return {
    index: options.index ?? 0,
    connected: options.connected ?? true,
    mapping: options.mapping ?? "standard",
    timestamp: 1,
    buttons: Array.from({ length: 17 }, (_, index) => ({
      pressed: pressed.has(index),
      touched: pressed.has(index),
      value: pressed.has(index) ? 1 : 0,
    })),
    axes: options.axes ?? [0, 0, 0, 0],
  };
}

function claimAndReady(snapshot = pad({ pressed: [12] })) {
  const claimed = updateNavigationController(initialNavigationControllerState(), [snapshot], 0);
  const neutral = pad({ index: snapshot.index });
  const gating = updateNavigationController(claimed.state, [neutral], 10);
  return updateNavigationController(gating.state, [neutral], 130).state;
}

describe("controller navigation model", () => {
  it("accepts only finite standard mappings with a dpad or left stick", () => {
    expect(isStandardNavigationController(pad())).toBe(true);
    expect(isStandardNavigationController(pad({ mapping: "" }))).toBe(false);
    expect(isStandardNavigationController({ ...pad(), axes: [Number.NaN, 0], buttons: pad().buttons.slice(0, 12) })).toBe(false);
  });

  it("claims the first active controller without executing its first action", () => {
    const first = pad({ index: 1, pressed: [0] });
    const result = updateNavigationController(initialNavigationControllerState(), [null, first], 0);
    expect(result.actions).toEqual([{ type: "claimed", index: 1, centerButtonObservable: true }]);
    expect(result.state.phase).toBe("neutral-gate");
  });

  it("requires 120ms of neutral input before navigation", () => {
    const claimed = updateNavigationController(initialNavigationControllerState(), [pad({ pressed: [0] })], 0);
    const firstNeutral = updateNavigationController(claimed.state, [pad()], 20);
    expect(firstNeutral.actions).toEqual([]);
    const ready = updateNavigationController(firstNeutral.state, [pad()], 140);
    expect(ready.actions).toEqual([{ type: "ready", index: 0 }]);
  });

  it("uses button edges and deterministic direction repeat timing", () => {
    let state = claimAndReady();
    let result = updateNavigationController(state, [pad({ pressed: [12] })], 200);
    expect(result.actions).toEqual([{ type: "direction", direction: "up" }]);
    state = result.state;
    result = updateNavigationController(state, [pad({ pressed: [12] })], 559);
    expect(result.actions).toEqual([]);
    result = updateNavigationController(result.state, [pad({ pressed: [12] })], 560);
    expect(result.actions).toEqual([{ type: "direction", direction: "up" }]);
    state = updateNavigationController(result.state, [pad()], 600).state;
    expect(updateNavigationController(state, [pad({ pressed: [0] })], 610).actions).toEqual([{ type: "confirm" }]);
    expect(updateNavigationController(state, [pad({ pressed: [9] })], 610).actions).toEqual([{ type: "navigation" }]);
  });

  it("applies axis hysteresis, axis lock and immediate reversal", () => {
    let state = claimAndReady();
    let result = updateNavigationController(state, [pad({ axes: [0.7, 0.65] })], 200);
    expect(result.actions).toEqual([{ type: "direction", direction: "right" }]);
    result = updateNavigationController(result.state, [pad({ axes: [0.4, 0.8] })], 250);
    expect(result.state.axis.direction).toBe("right");
    state = updateNavigationController(result.state, [pad()], 300).state;
    result = updateNavigationController(state, [pad({ axes: [-0.8, 0] })], 310);
    expect(result.actions).toEqual([{ type: "direction", direction: "left" }]);
  });

  it("disconnects anonymously and re-enters the neutral gate after suspension", () => {
    const state = claimAndReady();
    const disconnected = updateNavigationController(state, [], 200);
    expect(disconnected.actions).toEqual([{ type: "disconnected", index: 0 }]);
    const suspended = suspendNavigationController(state);
    expect(suspended.phase).toBe("neutral-gate");
    expect(suspended.buttons).toEqual([]);
  });
});
