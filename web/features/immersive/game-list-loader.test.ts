import { describe, expect, it, vi } from "vitest";
import type { ImmersiveGame, ImmersiveGameList } from "./api";
import { fetchInitialGameList } from "./game-list-loader";

function game(gameId: string): ImmersiveGame {
  return {
    gameId,
    title: gameId,
    description: "",
    releaseYear: null,
    developer: "",
    genre: "",
    platformInstance: { id: "gba-directory", name: "GBA 游戏" },
    defaultCore: { id: "mgba", name: "mGBA" },
    coverUrl: null,
    videoUrl: null,
    lastPlayedAtMs: null,
  };
}

function page(items: ImmersiveGame[], nextCursor: string | null): ImmersiveGameList {
  return {
    generatedAtMs: 1,
    platform: {
      platformId: "gba",
      platformName: "Game Boy Advance",
      gameCount: 3,
      lastPlayedAtMs: null,
    },
    items,
    nextCursor,
  };
}

describe("immersive initial game loading", () => {
  it("reads pages sequentially until the return game is present", async () => {
    const fetchPage = vi.fn()
      .mockResolvedValueOnce(page([game("a")], "cursor-1"))
      .mockResolvedValueOnce(page([game("b")], "cursor-2"))
      .mockResolvedValueOnce(page([game("target")], null));
    const result = await fetchInitialGameList(
      "gba",
      "target",
      new AbortController().signal,
      fetchPage,
    );
    expect(fetchPage.mock.calls.map((call) => call[1])).toEqual([
      null,
      "cursor-1",
      "cursor-2",
    ]);
    expect(result.items.map((item) => item.gameId)).toEqual(["a", "b", "target"]);
    expect(result.nextCursor).toBeNull();
  });

  it("does not prefetch when no return game was requested", async () => {
    const fetchPage = vi.fn().mockResolvedValue(page([game("a")], "cursor-1"));
    const result = await fetchInitialGameList(
      "gba",
      undefined,
      new AbortController().signal,
      fetchPage,
    );
    expect(fetchPage).toHaveBeenCalledOnce();
    expect(result.nextCursor).toBe("cursor-1");
  });
});
