import { describe, expect, it } from "vitest";
import { lockstepDelayBaseline } from "./netplay-buffer-baseline";

describe("delay injection baseline", () => {
  it("waits for startup RTT inflation to recover before injecting 100ms delay", () => {
    expect(lockstepDelayBaseline([
      { kind: "lockstep", frame: 0, inputBufferFrames: 1 },
      { kind: "lockstep", frame: 119, inputBufferFrames: 7 },
    ])).toBeNull();
  });
  it("uses the most recent recovered sample without synthesizing missing evidence", () => {
    const recovered = { kind: "lockstep", frame: 839, inputBufferFrames: 2 };
    expect(lockstepDelayBaseline([
      { kind: "lockstep", frame: 119, inputBufferFrames: 8 }, recovered,
      { kind: "canonical", frame: 840 },
    ])).toEqual(recovered);
    expect(lockstepDelayBaseline([])).toBeNull();
    expect(lockstepDelayBaseline([{ kind: "lockstep", frame: 0 }])).toBeNull();
  });
});
