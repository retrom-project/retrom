import { describe, expect, it } from "vitest";
import { formatTime } from "./backend";

describe("formatTime", () => {
  it("handles a missing timestamp without consulting the current clock", () => {
    expect(formatTime(null)).toBe("尚无记录");
  });

  it("formats a fixed Unix millisecond timestamp", () => {
    expect(formatTime(1_700_000_000_000)).toContain("2023");
  });
});
