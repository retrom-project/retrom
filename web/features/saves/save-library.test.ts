import { describe, expect, it } from "vitest";
import {
  collectSavePages,
  customSaveName,
  filterSaveItems,
  formatSaveDuration,
  formatSaveSize,
  formatSaveTime,
  groupSaveItems,
  latestAvailableSave,
  saveLibraryStats,
  type SaveItem,
} from "./save-library";

const nowMs = new Date(2026, 7, 8, 12, 0).getTime();

function save(overrides: Partial<SaveItem> & Pick<SaveItem, "saveStateId" | "gameId" | "gameTitle">): SaveItem {
  return {
    name: "手动存档 2026/8/8",
    version: 1,
    createdAtMs: nowMs,
    activeDurationMs: 60_000,
    sizeBytes: 800 * 1024,
    screenshotUrl: "/shot",
    core: { id: "fbneo", name: "FinalBurn Neo" },
    platform: { id: "arcade", name: "Arcade" },
    platformInstance: { id: "instance", name: "FBNeo 游戏" },
    availability: { status: "AVAILABLE", reasons: [] },
    ...overrides,
  };
}

describe("save library projection", () => {
  const saves = [
    save({ saveStateId: "new", gameId: "1943", gameTitle: "1943", createdAtMs: nowMs - 1_000 }),
    save({ saveStateId: "old", gameId: "1941", gameTitle: "1941", name: "Boss 前", createdAtMs: nowMs - 2_000, availability: { status: "BLOCKED", reasons: [] } }),
    save({ saveStateId: "older", gameId: "1941", gameTitle: "1941", createdAtMs: nowMs - 3_000 }),
  ];

  it("filters by search, game and availability with stable chronological sorting", () => {
    expect(filterSaveItems(saves, { query: "Boss", gameId: "", availability: "ALL", sort: "CREATED_DESC" }).map((item) => item.saveStateId)).toEqual(["old"]);
    expect(filterSaveItems(saves, { query: "", gameId: "1941", availability: "AVAILABLE", sort: "CREATED_ASC" }).map((item) => item.saveStateId)).toEqual(["older"]);
  });

  it("groups the filtered order by game and keeps aggregate statistics independent", () => {
    const groups = groupSaveItems(saves);
    expect(groups.map((group) => [group.gameId, group.saves.length])).toEqual([["1943", 1], ["1941", 2]]);
    expect(saveLibraryStats(saves)).toEqual({ saveCount: 3, gameCount: 2 });
    expect(latestAvailableSave(saves)?.saveStateId).toBe("new");
  });

  it("formats relative timestamps, duration and generated names for compact cards", () => {
    expect(formatSaveTime(new Date(2026, 7, 8, 1, 2, 3).getTime(), nowMs)).toBe("今天 01:02:03");
    expect(formatSaveTime(new Date(2026, 7, 7, 21, 9).getTime(), nowMs, false)).toBe("昨天 21:09");
    expect(formatSaveDuration(7_440_000)).toBe("2 小时 4 分");
    expect(customSaveName("手动存档 2026/8/8 01:02")).toBeNull();
    expect(customSaveName("Boss 前")).toBe("Boss 前");
  });

  it("formats save payload sizes with compact binary units and at most two decimals", () => {
    expect(formatSaveSize(800 * 1024)).toBe("800KB");
    expect(formatSaveSize(Math.round(1.23 * 1024 * 1024))).toBe("1.23MB");
    expect(formatSaveSize(Math.round(30.32 * 1024 * 1024))).toBe("30.32MB");
  });

  it("collects every cursor page and preserves the first response clock", async () => {
    const cursors: Array<string | null> = [];
    const result = await collectSavePages(async (cursor) => {
      cursors.push(cursor);
      return cursor === null
        ? { generatedAtMs: 101, items: [saves[0]], nextCursor: "page-2" }
        : { generatedAtMs: 202, items: [saves[1]], nextCursor: null };
    });

    expect(cursors).toEqual([null, "page-2"]);
    expect(result.generatedAtMs).toBe(101);
    expect(result.items.map((item) => item.saveStateId)).toEqual(["new", "old"]);
  });

  it("rejects a repeated cursor instead of looping forever", async () => {
    await expect(collectSavePages(async () => ({ generatedAtMs: 101, items: [], nextCursor: "same" })))
      .rejects.toThrow("repeated save cursor");
  });
});
