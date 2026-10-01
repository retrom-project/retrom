import { describe, expect, it } from "vitest";
import { recentGameURL, formatRecentDuration, zonedDateKey } from "./recent-games";

const nowMs = new Date("2026-08-08T12:00:00+08:00").getTime();

describe("recent game paging", () => {
  it("sends bounded server filters and an exact rolling-window cutoff", () => {
    const url = new URL(recentGameURL({ query: " mame ", platformId: "arcade", sort: "sessions", period: "7d", nowMs }), "http://test");
    expect(Object.fromEntries(url.searchParams)).toEqual({ limit: "50", q: "mame", platformId: "arcade", sort: "SESSIONS_DESC", fromAtMs: String(nowMs - 7 * 86_400_000) });
  });
  it("formats durations and groups days in the browser timezone", () => {
    expect(formatRecentDuration(59_000)).toBe("少于 1 分钟");
    expect(formatRecentDuration(7_440_000)).toBe("2 小时 4 分");
    expect(zonedDateKey(new Date("2026-08-08T01:00:00Z").getTime(), "America/Los_Angeles")).toBe("2026-08-07");
  });
});
