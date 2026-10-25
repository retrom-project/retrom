import { afterEach, describe, expect, it, vi } from "vitest";
import { entryId, loadDestinations, loadEntries, platformCode } from "./immersive-data";
import { saveFixture } from "./test-fixtures";

const requests = vi.hoisted(() => ({ GET: vi.fn(), loadGames: vi.fn(), loadDirectories: vi.fn() }));
vi.mock("@/lib/api/client", () => ({ api: requests, result: (response: { data: unknown }) => response.data }));
vi.mock("@/features/library/api", () => requests);
afterEach(() => vi.resetAllMocks());

it("requests game groups before pagination and uses the server's game total", async () => {
  const save = saveFixture("latest", "game-25");
  requests.GET.mockResolvedValue({ data: { items: [save], offset: 24, limit: 24, total: 25, gameCount: 25 } });
  const entries = await loadEntries("saves", undefined, "", 24);
  expect(requests.GET).toHaveBeenCalledWith("/api/v1/saves", { params: { query: { offset: 24, limit: 24, groupByGame: true } }, signal: undefined });
  expect(entries).toEqual({ items: [{ game: save.game, save }], offset: 24, limit: 24, total: 25, gameCount: 25 });
  expect(entryId(entries.items[0])).toBe("game-25");
  expect(entryId({ game: save.game, save: saveFixture("newer", "game-25") })).toBe("game-25");
});

it("uses grouped games for the save destination count and featured covers", async () => {
  const saves = [saveFixture("save-1", "first"), saveFixture("save-2", "second"), saveFixture("save-3", "third")];
  requests.loadDirectories.mockResolvedValue({ items: [] });
  requests.loadGames.mockResolvedValue({ items: [saves[0].game], total: 3 });
  requests.GET.mockImplementation(async (path) => ({ data: path === "/api/v1/saves" ? { items: saves, total: 25, offset: 0 } : { items: [], total: 0, offset: 0 } }));
  const destinations = await loadDestinations();
  const destination = destinations.items.find((item) => item.id === "saves");
  expect(destination?.count).toBe(25);
  expect(destination?.featured.map((game) => game.id)).toEqual(["first", "second", "third"]);
  expect(requests.GET).toHaveBeenCalledWith("/api/v1/saves", { params: { query: { offset: 0, limit: 3, groupByGame: true } }, signal: undefined });
});

it("hides directories without games and keeps empty library entries", async () => {
  requests.loadDirectories.mockResolvedValue({ items: [
    { id: "nes", name: "FC 红白机", platformId: "nes", gameCount: 2 },
    { id: "gba", name: "GBA 游戏", platformId: "gba", gameCount: 0 },
  ] });
  requests.loadGames.mockImplementation(async (scope) => ({ items: [], total: scope === "library" ? 2 : 0 }));
  requests.GET.mockResolvedValue({ data: { items: [], total: 0, offset: 0 } });
  const destinations = await loadDestinations();
  expect(destinations.items.map((item) => item.id)).toEqual(["all", "recent", "favorites", "saves", "nes"]);
});

describe("immersive platform codes", () => {
  it("names the base model of a shared platform family and otherwise keeps the platform ID", () => {
    expect(platformCode("Game Boy 游戏", "gbc")).toBe("GB");
    expect(platformCode("Game Boy Color 游戏", "gbc")).toBe("GBC");
    expect(platformCode("GBC 游戏", "gbc")).toBe("GBC");
    expect(platformCode("掌机", "gbc")).toBe("GBC");
    expect(platformCode("GBA 游戏", "gba")).toBe("GBA");
    expect(platformCode("Neo Geo Pocket 游戏", "ngpc")).toBe("NGP");
    expect(platformCode("Game Gear 游戏", "gamegear")).toBe("GAMEGEAR");
    expect(platformCode("RPG Maker 游戏", "rpgmaker")).toBe("RPGMAKER");
    expect(platformCode("FC 红白机", "nes")).toBe("NES");
  });
});
