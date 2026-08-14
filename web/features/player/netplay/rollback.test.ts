import { describe, expect, it } from "vitest";
import { NETPLAY_CONTROL_COUNT, RollbackTimeline, predictInputs, type CanonicalInput } from "./rollback";

function controls(value = 0) { return Array<number>(NETPLAY_CONTROL_COUNT).fill(value); }
function input(p1 = 0, p2 = 0): CanonicalInput { return [controls(p1), controls(p2), controls(), controls()]; }

describe("RollbackTimeline", () => {
  it("predicts only eight frames ahead of contiguous canonical history", () => {
    const timeline = new RollbackTimeline();
    expect(timeline.canPredict(7)).toBe(true);
    expect(timeline.canPredict(8)).toBe(false);
    timeline.recordBefore(0, new Uint8Array([1]));
    timeline.recordPrediction(0, input(1));
    expect(timeline.receiveCanonical(0, input(1))).toBeNull();
    expect(timeline.canPredict(8)).toBe(true);
    expect(timeline.canPredict(9)).toBe(false);
    timeline.reset(605);
    expect(timeline.canPredict(612)).toBe(true);
    expect(timeline.canPredict(613)).toBe(false);
  });

  it("supports a core-specific prediction limit", () => {
    const timeline = new RollbackTimeline(120, undefined, 1);
    expect(timeline.canPredict(0)).toBe(true);
    expect(timeline.canPredict(1)).toBe(false);
    timeline.receiveCanonical(0, input());
    expect(timeline.canPredict(1)).toBe(true);
    expect(timeline.canPredict(2)).toBe(false);
  });

  it("accepts strict lockstep as a zero-frame prediction profile", () => {
    const timeline = new RollbackTimeline(120, undefined, 0);
    expect(timeline.canPredict(0)).toBe(false);
    timeline.receiveCanonical(0, input());
    expect(timeline.canPredict(0)).toBe(true);
    expect(timeline.canPredict(1)).toBe(false);
  });

  it("returns the earliest mismatched frame and builds deterministic replay", () => {
    const timeline = new RollbackTimeline();
    for (let frame = 0; frame < 3; frame += 1) {
      timeline.recordBefore(frame, new Uint8Array([frame + 1]));
      timeline.recordPrediction(frame, input(1, 0));
    }
    expect(timeline.receiveCanonical(0, input(1, 0))).toBeNull();
    expect(timeline.receiveCanonical(1, input(1, 1))).toBe(1);
    expect(timeline.receiveCanonical(2, input(1, 0))).toBeNull();
    expect(timeline.earliestDifference(0, 2)).toBe(1);
    expect(timeline.rollbackPlan(1, 2)).toEqual({
      state: new Uint8Array([2]),
      frames: [{ frame: 1, input: input(1, 1) }, { frame: 2, input: input(1, 0) }],
    });
  });

  it("fails closed on mutated canonical history, gaps, window overflow, and ring capacity", () => {
    const timeline = new RollbackTimeline(1, 2);
    timeline.recordBefore(0, new Uint8Array([1]));
    timeline.recordPrediction(0, input());
    timeline.receiveCanonical(0, input());
    expect(() => timeline.receiveCanonical(0, input(1))).toThrow("NETPLAY_CANONICAL_MUTATED");
    expect(() => timeline.rollbackPlan(0, 2)).toThrow("ROLLBACK_WINDOW_EXCEEDED");
    expect(() => timeline.recordBefore(1, new Uint8Array([1, 2]))).toThrow("STATE_RING_CAPACITY_EXCEEDED");
  });
});

describe("predictInputs", () => {
  it("keeps remote prediction while replacing only the local controls", () => {
    const previous = input(1, 2);
    const result = predictInputs(previous, 2, controls(9));
    expect(result[0]).toEqual(controls(1));
    expect(result[1]).toEqual(controls(9));
  });
});
