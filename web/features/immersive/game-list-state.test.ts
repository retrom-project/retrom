import { describe, expect, it } from "vitest";
import type { ImmersiveGame } from "./api";
import {
  initialGameIndex,
  mergeGamePage,
  moveGameIndex,
  pageGameIndex,
  selectionAfterRemoval,
  shouldPrefetchGamePage,
} from "./game-list-state";

function item(gameId: string): ImmersiveGame {
  return {
    gameId,
    title: gameId,
    titleInitial: gameId.slice(0, 1).toUpperCase(),
    description: "",
    releaseYear: null,
    developer: "",
    genre: "",
    platformInstance: { id: "gba", name: "GBA" },
    defaultCore: { id: "mgba", name: "mGBA" },
    coverUrl: null,
    videoUrl: null,
    lastPlayedAtMs: null,
    favorited: false,
    saveStates: [],
  };
}

describe("immersive game list state", () => {
  it("restores a valid game hint and otherwise selects the first game", () => {
    const games = [item("a"), item("b")];
    expect(initialGameIndex(games, "b")).toBe(1);
    expect(initialGameIndex(games, "missing")).toBe(0);
    expect(initialGameIndex([], "missing")).toBe(-1);
  });

  it("does not wrap at either list boundary", () => {
    expect(moveGameIndex(0, "up", 3)).toBe(0);
    expect(moveGameIndex(2, "down", 3)).toBe(2);
    expect(moveGameIndex(1, "up", 3)).toBe(0);
  });

  it("pages eight games with left and right while clamping to the list", () => {
    expect(pageGameIndex(0, "right", 20)).toBe(8);
    expect(pageGameIndex(8, "right", 20)).toBe(16);
    expect(pageGameIndex(16, "right", 20)).toBe(19);
    expect(pageGameIndex(19, "left", 20)).toBe(11);
    expect(pageGameIndex(3, "left", 20)).toBe(0);
  });

  it("merges cursor pages without duplicating an existing game", () => {
    expect(mergeGamePage([item("a"), item("b")], [item("b"), item("c")]).map((game) => game.gameId)).toEqual(["a", "b", "c"]);
  });

  it("prefetches only within the final ten loaded games", () => {
    expect(shouldPrefetchGamePage(39, 50, "next")).toBe(false);
    expect(shouldPrefetchGamePage(40, 50, "next")).toBe(true);
    expect(shouldPrefetchGamePage(49, 50, null)).toBe(false);
  });

  it("keeps the same item or chooses its adjacent index after deletion", () => {
    const previous = [item("a"), item("b"), item("c")];
    expect(selectionAfterRemoval(previous, [item("a"), item("b")], "b")).toBe(1);
    expect(selectionAfterRemoval(previous, [item("a"), item("c")], "b")).toBe(1);
    expect(selectionAfterRemoval(previous, [], "b")).toBe(-1);
  });
});
