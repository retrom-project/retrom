import { describe, expect, it } from "vitest";
import { formatBytes, formatTime } from "./backend";

describe("formatTime", () => {
  it("handles a missing timestamp without consulting the current clock", () => {
    expect(formatTime(null)).toBe("尚无记录");
  });

  it("formats a fixed Unix millisecond timestamp", () => {
    expect(formatTime(1_700_000_000_000)).toContain("2023");
  });
});

describe("formatBytes", () => {
  it("uses compact human-readable binary units", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1023)).toBe("1023 B");
    expect(formatBytes(1024)).toBe("1 KB");
    expect(formatBytes(805_560)).toBe("786.7 KB");
    expect(formatBytes(5 * 1024 * 1024)).toBe("5 MB");
  });

  it("does not render invalid byte counts", () => {
    expect(formatBytes(-1)).toBe("—");
    expect(formatBytes(Number.NaN)).toBe("—");
  });
});
