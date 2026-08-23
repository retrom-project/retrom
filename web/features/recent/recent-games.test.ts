import { describe, expect, it } from "vitest";
import { filterRecentGames, formatRecentDuration, recentGameStats, type RecentGame } from "./recent-games";

const nowMs = new Date("2026-08-08T12:00:00+08:00").getTime();

function game(overrides: Partial<RecentGame> & Pick<RecentGame, "gameId" | "title">): RecentGame {
  return {
    status: "PUBLISHED",
    availability: "PUBLISHED",
    platform: { id: "arcade", name: "街机" },
    platformInstance: { id: "fbneo", name: "FBNeo 游戏" },
    lastPlayedAtMs: nowMs,
    activeDurationMs: 60_000,
    sessionCount: 1,
    coverUrl: null,
    ...overrides,
  };
}

describe("recent game projection", () => {
  const games = [
    game({ gameId: "b", title: "1941", lastPlayedAtMs: nowMs - 8 * 86_400_000, activeDurationMs: 240_000, sessionCount: 8 }),
    game({ gameId: "a", title: "1943: The Battle of Midway", lastPlayedAtMs: nowMs - 60_000, platform: { id: "nes", name: "NES" } }),
    game({ gameId: "c", title: "Metal Slug", lastPlayedAtMs: nowMs - 7 * 86_400_000, activeDurationMs: 120_000, platformInstance: { id: "mame", name: "MAME 收藏" } }),
  ];

  it("filters across game and directory names without imposing a result limit", () => {
    expect(filterRecentGames(games, { query: "mame", platformId: "", sort: "recent", period: "all", nowMs }).map((item) => item.gameId)).toEqual(["c"]);
    expect(filterRecentGames(Array.from({ length: 75 }, (_, index) => game({ gameId: String(index), title: `Game ${index}` })), { query: "", platformId: "", sort: "recent", period: "all", nowMs })).toHaveLength(75);
  });

  it("keeps the exact seven-day boundary and supports platform and aggregate sorting", () => {
    expect(filterRecentGames(games, { query: "", platformId: "", sort: "recent", period: "7d", nowMs }).map((item) => item.gameId)).toEqual(["a", "c"]);
    expect(filterRecentGames(games, { query: "", platformId: "arcade", sort: "sessions", period: "all", nowMs }).map((item) => item.gameId)).toEqual(["b", "c"]);
  });

  it("summarizes the full history and formats durations compactly", () => {
    expect(recentGameStats(games)).toEqual({ gameCount: 3, activeDurationMs: 420_000, sessionCount: 10 });
    expect(formatRecentDuration(59_000)).toBe("少于 1 分钟");
    expect(formatRecentDuration(7_440_000)).toBe("2 小时 4 分");
  });
});
