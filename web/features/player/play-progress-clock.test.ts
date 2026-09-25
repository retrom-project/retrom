import {describe, expect, it} from "vitest";
import {PlayProgressClock} from "./play-progress-clock";

describe("PlayProgressClock", () => {
  it("reports cumulative visible running time across hidden and paused intervals", () => {
    const clock = new PlayProgressClock();
    clock.start(0, true);
    expect(clock.snapshot(10_000)).toBe(10_000);
    clock.setVisible(15_000, false);
    expect(clock.snapshot(50_000)).toBe(15_000);
    clock.setVisible(50_000, true);
    clock.setPaused(60_000, true);
    expect(clock.snapshot(90_000)).toBe(25_000);
    clock.setPaused(90_000, false);
    expect(clock.snapshot(100_000)).toBe(35_000);
    clock.stop(110_000);
    expect(clock.snapshot(120_000)).toBe(45_000);
  });
});
