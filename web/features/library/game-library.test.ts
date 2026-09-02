import { describe, expect, it } from "vitest";
import {
  filterLibraryGames,
  formatLibraryPlayedAt,
  gamePageQuery,
  libraryPlatformInstances,
  libraryPlatforms,
  type GameSummary,
} from "./game-library";

const nowMs = new Date(2026, 7, 8, 12, 0).getTime();

function game(overrides: Partial<GameSummary> & Pick<GameSummary, "gameId" | "title">): GameSummary {
  return {
    platform: { id: "arcade", name: "Arcade" },
    platformInstance: { id: "fbneo", name: "FBNeo 游戏" },
    defaultCore: { id: "fbneo", name: "FinalBurn Neo" },
    status: "PUBLISHED",
    coverUrl: null,
    createdAtMs: nowMs - 10_000,
    lastPlayedAtMs: null,
    favorite: null,
    ...overrides,
  };
}

describe("game library projection", () => {
  const games = [
    game({ gameId: "1941", title: "1941", createdAtMs: nowMs - 30_000, lastPlayedAtMs: nowMs - 2_000, tags: [{ tagId: "arcade-action", name: "动作" }] }),
    game({ gameId: "1943", title: "1943", createdAtMs: nowMs - 20_000, lastPlayedAtMs: nowMs - 1_000, tags: [{ tagId: "arcade-action", name: "动作" }, { tagId: "coop", name: "双人合作" }] }),
    game({ gameId: "doom", title: "DOOM", platform: { id: "dos", name: "MS-DOS" }, platformInstance: { id: "dos-classics", name: "DOS 经典" }, defaultCore: { id: "dosbox_pure", name: "DOSBox Pure" }, createdAtMs: nowMs - 5_000 }),
  ];

  it("filters locally and applies all three stable sort modes", () => {
    expect(filterLibraryGames(games, { query: "FinalBurn", platformId: "", platformInstanceId: "", sort: "RECENT_DESC" }).map((item) => item.gameId)).toEqual(["1943", "1941"]);
    expect(filterLibraryGames(games, { query: "", platformId: "dos", platformInstanceId: "dos-classics", sort: "ADDED_DESC" }).map((item) => item.gameId)).toEqual(["doom"]);
    expect(filterLibraryGames(games, { query: "", platformId: "", platformInstanceId: "", sort: "TITLE_ASC" }).map((item) => item.gameId)).toEqual(["1941", "1943", "doom"]);
    expect(filterLibraryGames(games, { query: "合作", platformId: "", platformInstanceId: "", sort: "TITLE_ASC" }).map((item) => item.gameId)).toEqual(["1943"]);
    expect(filterLibraryGames(games, { query: "", platformId: "", platformInstanceId: "", tagId: "arcade-action", sort: "TITLE_ASC" }).map((item) => item.gameId)).toEqual(["1941", "1943"]);
  });

  it("builds deterministic platform counts and dependent collections", () => {
    expect(libraryPlatforms(games)).toEqual([{ id: "arcade", name: "Arcade", count: 2 }, { id: "dos", name: "MS-DOS", count: 1 }]);
    expect(libraryPlatformInstances(games, "arcade")).toEqual([{ id: "fbneo", name: "FBNeo 游戏", platformId: "arcade" }]);
  });

  it("formats never-played and relative play times", () => {
    expect(formatLibraryPlayedAt(null, nowMs)).toBe("尚未游玩");
    expect(formatLibraryPlayedAt(new Date(2026, 7, 8, 1, 20).getTime(), nowMs)).toBe("今天 01:20");
    expect(formatLibraryPlayedAt(new Date(2026, 7, 7, 21, 10).getTime(), nowMs)).toBe("昨天 21:10");
  });

  it("uses the requested browser timezone for local-day labels", () => {
    const generatedAtMs = Date.parse("2026-09-02T16:30:00.000Z");
    const playedAtMs = Date.parse("2026-09-02T15:30:00.000Z");
    expect(formatLibraryPlayedAt(playedAtMs, generatedAtMs, "Asia/Shanghai")).toBe("昨天 23:30");
    expect(formatLibraryPlayedAt(playedAtMs, generatedAtMs, "America/New_York")).toBe("今天 11:30");
  });

  it("builds a fixed 50-item cursor query with all server-side filters", () => {
    expect(gamePageQuery({ query: "  doom ", platformId: "dos", platformInstanceId: "dos-classics", tagId: "solo", sort: "ADDED_DESC" }, "next page"))
      .toBe("limit=50&sort=ADDED_DESC&q=doom&platformId=dos&platformInstanceId=dos-classics&tagId=solo&cursor=next+page");
  });
});
