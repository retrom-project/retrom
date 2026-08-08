import { describe, expect, it } from "vitest";
import {
  canReorderPlatformDirectories,
  filterPlatformDirectories,
  platformDirectorySummary,
  type PlatformDirectoryFilters,
  type PlatformInstance,
} from "./platform-directory-list";

function directory(overrides: Partial<PlatformInstance> & Pick<PlatformInstance, "id" | "name">): PlatformInstance {
  return {
    platformId: "arcade",
    platformName: "Arcade",
    defaultCoreId: "fbneo",
    defaultCoreName: "FinalBurn Neo",
    slug: overrides.id,
    description: "街机游戏集合",
    sortOrder: 100,
    enabled: true,
    version: 1,
    gameCount: 0,
    ...overrides,
  };
}

const baseFilters: PlatformDirectoryFilters = { query: "", platformId: "", status: "ALL", sort: "ORDER" };
const directories = [
  directory({ id: "fbneo", name: "FBNeo 游戏", sortOrder: 200, gameCount: 3 }),
  directory({ id: "mame", name: "MAME 游戏", sortOrder: 100, enabled: false }),
  directory({ id: "gba", name: "GBA 游戏", platformId: "gba", platformName: "Game Boy Advance", defaultCoreId: "mgba", defaultCoreName: "mGBA", description: "掌机合集", sortOrder: 300 }),
];

describe("platform directory list", () => {
  it("summarizes enabled, disabled, empty, and arcade directories", () => {
    expect(platformDirectorySummary(directories)).toEqual({ total: 3, enabled: 2, disabled: 1, empty: 2, arcade: 2 });
  });

  it("searches user-facing directory, platform, and description text", () => {
    expect(filterPlatformDirectories(directories, { ...baseFilters, query: "掌机" }).map((item) => item.id)).toEqual(["gba"]);
    expect(filterPlatformDirectories(directories, { ...baseFilters, query: "arcade" }).map((item) => item.id)).toEqual(["mame", "fbneo"]);
  });

  it("filters status and sorts deterministically", () => {
    expect(filterPlatformDirectories(directories, { ...baseFilters, status: "DISABLED" }).map((item) => item.id)).toEqual(["mame"]);
    expect(filterPlatformDirectories(directories, { ...baseFilters, sort: "GAME_COUNT" }).map((item) => item.id)).toEqual(["fbneo", "gba", "mame"]);
    expect(filterPlatformDirectories(directories, { ...baseFilters, sort: "NAME" }).map((item) => item.id)).toEqual(["fbneo", "gba", "mame"]);
  });

  it("only allows global reordering in the unfiltered display-order view", () => {
    expect(canReorderPlatformDirectories(baseFilters)).toBe(true);
    expect(canReorderPlatformDirectories({ ...baseFilters, query: "gba" })).toBe(false);
    expect(canReorderPlatformDirectories({ ...baseFilters, sort: "NAME" })).toBe(false);
  });
});
