import { describe, expect, it } from "vitest";
import {
  adminGameDirectories,
  adminGameSummary,
  collectAdminGamePages,
  filterAdminGames,
  runtimePresentation,
  type AdminGameSummary,
} from "./admin-game-library";

function game(overrides: Partial<AdminGameSummary> & Pick<AdminGameSummary, "gameId" | "title">): AdminGameSummary {
  return {
    platform: { id: "arcade", name: "Arcade" },
    platformInstance: { id: "fbneo", name: "FBNeo 游戏" },
    defaultCore: { id: "fbneo", name: "FinalBurn Neo" },
    status: "PUBLISHED",
    coverUrl: "/cover.png",
    createdAtMs: 100,
    lastPlayedAtMs: null,
    favorite: null,
    version: 1,
    updatedAtMs: 100,
    releaseYear: 1990,
    metadataComplete: true,
    runtimeStatus: "READY",
    ...overrides,
  };
}

describe("admin game library", () => {
  const games = [
    game({ gameId: "a", title: "1943", updatedAtMs: 300 }),
    game({ gameId: "b", title: "Metal Slug", platformInstance: { id: "neo", name: "Neo Geo" }, runtimeStatus: null, metadataComplete: false, coverUrl: null, updatedAtMs: 200 }),
    game({ gameId: "c", title: "Final Fight", status: "DELETED", updatedAtMs: 400 }),
  ];

  it("summarizes management health independently of active filters", () => {
    expect(adminGameSummary(games)).toEqual({ total: 3, runtimeAttention: 1, missingCover: 1, incompleteMetadata: 1, hidden: 1 });
  });

  it("filters by dependent directory, visibility, runtime, and immediate text", () => {
    expect(filterAdminGames(games, { query: "metal", platformId: "arcade", platformInstanceId: "neo", visibility: "PUBLISHED", runtime: "ATTENTION", sort: "UPDATED_DESC" }).map((item) => item.gameId)).toEqual(["b"]);
    expect(adminGameDirectories(games, "arcade").map((item) => item.id)).toEqual(["fbneo", "neo"]);
  });

  it("sorts deterministically by update, addition, or title", () => {
    const base = { query: "", platformId: "", platformInstanceId: "", visibility: "ALL" as const, runtime: "ALL" as const };
    expect(filterAdminGames(games, { ...base, sort: "UPDATED_DESC" }).map((item) => item.gameId)).toEqual(["c", "a", "b"]);
    expect(filterAdminGames(games, { ...base, sort: "TITLE_ASC" }).map((item) => item.gameId)).toEqual(["a", "c", "b"]);
  });

  it("maps runtime states to user-facing health labels", () => {
    expect(runtimePresentation("READY").label).toBe("可以运行");
    expect(runtimePresentation(null).label).toBe("待验证");
    expect(runtimePresentation("BLOCKED").label).toBe("需要处理");
  });

  it("collects every cursor page and rejects repeated cursors", async () => {
    const pages = new Map<string | null, { generatedAtMs: number; items: AdminGameSummary[]; nextCursor: string | null }>([
      [null, { generatedAtMs: 500, items: [games[0]], nextCursor: "next" }],
      ["next", { generatedAtMs: 501, items: [games[1]], nextCursor: null }],
    ]);
    await expect(collectAdminGamePages(async (cursor) => pages.get(cursor)!)).resolves.toEqual({ generatedAtMs: 500, items: [games[0], games[1]] });
    await expect(collectAdminGamePages(async () => ({ generatedAtMs: 500, items: [], nextCursor: "same" }))).rejects.toThrow("repeated admin game cursor");
  });
});
