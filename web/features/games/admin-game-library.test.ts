import { describe, expect, it } from "vitest";
import {
  adminGameURL,
  collectAdminGameExport,
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
    game({ gameId: "c", title: "Final Fight", status: "DELETED", runtimeStatus: null, updatedAtMs: 400 }),
  ];

  it("sends filters and a bounded page size to the server", () => {
    const url = new URL(adminGameURL({ query: " Metal ", platformId: "arcade", platformInstanceId: "neo", tagId: "tag", visibility: "PUBLISHED", runtime: "ATTENTION", sort: "TITLE_ASC" }), "http://test");
    expect(Object.fromEntries(url.searchParams)).toEqual({ limit: "6", q: "Metal", platformId: "arcade", platformInstanceId: "neo", tagId: "tag", status: "PUBLISHED", runtime: "ATTENTION", sort: "TITLE_ASC" });
  });

  it("maps runtime states to user-facing health labels", () => {
    expect(runtimePresentation("READY").label).toBe("可以运行");
    expect(runtimePresentation(null).label).toBe("待验证");
    expect(runtimePresentation("BLOCKED").label).toBe("需要处理");
    expect(runtimePresentation("READY", "DELETED")).toEqual({ label: "已删除", tone: "bad", note: "游戏已删除" });
  });

  it("exports every cursor page on demand and rejects repeated cursors", async () => {
    const pages = new Map<string | null, { generatedAtMs: number; items: AdminGameSummary[]; nextCursor: string | null }>([
      [null, { generatedAtMs: 500, items: [games[0]], nextCursor: "next" }],
      ["next", { generatedAtMs: 501, items: [games[1]], nextCursor: null }],
    ]);
    await expect(collectAdminGameExport(async (cursor) => pages.get(cursor)!)).resolves.toEqual({ generatedAtMs: 500, items: [games[0], games[1]] });
    await expect(collectAdminGameExport(async () => ({ generatedAtMs: 500, items: [], nextCursor: "same" }))).rejects.toThrow("repeated admin game cursor");
  });
});
