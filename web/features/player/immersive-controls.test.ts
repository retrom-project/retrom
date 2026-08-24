import { describe, expect, it } from "vitest";
import {
  ImmersiveChordDetector,
  ImmersiveNeutralGate,
  gamepadButtonPressed,
  isNeutralGamepads,
  isStandardImmersiveGamepad,
  zeroGamepad,
  type GamepadLike,
} from "./immersive-controls";

function sample(detector: ImmersiveChordDetector, nowMs: number, select = false, start = false) {
  return detector.update(select, start, nowMs);
}

function chord(detector: ImmersiveChordDetector, startAtMs: number, gapMs = 40) {
  sample(detector, startAtMs, true, false);
  const recognized = sample(detector, startAtMs + gapMs, true, true);
  sample(detector, startAtMs + gapMs + 10, false, false);
  return recognized;
}

function button(value = 0): GamepadButton {
  return { pressed: value >= 0.5, touched: value > 0, value };
}

function gamepad(index = 0, mapping: GamepadMappingType = "standard"): GamepadLike {
  return {
    axes: [0, 0, 0, 0],
    buttons: Array.from({ length: 16 }, () => button()),
    connected: true,
    id: "not-observed-by-product",
    index,
    mapping,
    timestamp: 1,
  };
}

describe("immersive Select+Start detector", () => {
  it("delays an individual button for 100ms and emits a fast tap pulse", () => {
    const detector = new ImmersiveChordDetector();
    expect(sample(detector, 0, true).select).toBe(false);
    expect(sample(detector, 99, true).select).toBe(false);
    expect(sample(detector, 100, true).select).toBe(true);
    expect(sample(detector, 120, false).select).toBe(false);

    expect(sample(detector, 200, false, true).start).toBe(false);
    expect(sample(detector, 250).start).toBe(true);
    expect(sample(detector, 251).start).toBe(false);
  });

  it("suppresses one complete chord and opens only for a valid second chord", () => {
    const detector = new ImmersiveChordDetector();
    expect(chord(detector, 0)).toEqual({ openMenu: false, select: false, start: false });
    expect(chord(detector, 110)).toEqual({ openMenu: true, select: false, start: false });
  });

  it("accepts the exact 100/60/650ms boundaries", () => {
    const detector = new ImmersiveChordDetector();
    expect(chord(detector, 0, 100).openMenu).toBe(false);
    expect(chord(detector, 210, 100).openMenu).toBe(true);

    const lastBoundary = new ImmersiveChordDetector();
    chord(lastBoundary, 0);
    expect(chord(lastBoundary, 700).openMenu).toBe(true);
  });

  it("rejects early, late, and wider second chord timing", () => {
    const early = new ImmersiveChordDetector();
    chord(early, 0);
    expect(chord(early, 109).openMenu).toBe(false);

    const late = new ImmersiveChordDetector();
    chord(late, 0);
    expect(chord(late, 701).openMenu).toBe(false);

    const wide = new ImmersiveChordDetector();
    sample(wide, 0, true);
    expect(sample(wide, 101, true, true).openMenu).toBe(false);
  });

  it("clears a pending gesture without replaying the reserved chord", () => {
    const detector = new ImmersiveChordDetector();
    chord(detector, 0);
    expect(sample(detector, 900)).toEqual({ openMenu: false, select: false, start: false });
    detector.reset();
    expect(sample(detector, 1_000)).toEqual({ openMenu: false, select: false, start: false });
  });
});

describe("immersive gamepad gates", () => {
  it("requires 120ms of uninterrupted neutral input", () => {
    const gate = new ImmersiveNeutralGate();
    expect(gate.update(true, 0)).toBe(false);
    expect(gate.update(true, 119)).toBe(false);
    expect(gate.update(false, 120)).toBe(false);
    expect(gate.update(true, 200)).toBe(false);
    expect(gate.update(true, 320)).toBe(true);
  });

  it("validates standard pads and all-pad neutrality without reading the id", () => {
    const standard = gamepad();
    expect(isStandardImmersiveGamepad(standard)).toBe(true);
    expect(isStandardImmersiveGamepad(gamepad(1, ""))).toBe(false);
    expect(isNeutralGamepads([standard])).toBe(true);
    standard.axes = [0.36, 0];
    expect(isNeutralGamepads([standard])).toBe(false);
  });

  it("creates a complete zero snapshot and preserves the source object", () => {
    const source = gamepad();
    source.buttons = source.buttons.map((current, index) => index === 8 ? button(1) : current);
    source.axes = [0.75, -0.5];
    const zeroed = zeroGamepad(source);
    expect(gamepadButtonPressed(zeroed, 8)).toBe(false);
    expect(zeroed.axes).toEqual([0, 0]);
    expect(gamepadButtonPressed(source, 8)).toBe(true);
  });
});
