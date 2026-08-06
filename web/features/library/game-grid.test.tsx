import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { GameGrid, type GameSummary } from "./game-grid";

describe("GameGrid", () => {
  it("renders an actionable empty state", () => {
    render(<GameGrid games={[]} />);
    expect(screen.getByRole("heading", { name: "游戏库还是空的" })).toBeInTheDocument();
  });

  it("distinguishes an empty filtered result from an empty library", () => {
    render(<GameGrid games={[]} filtered />);
    expect(screen.getByRole("heading", { name: "没有找到游戏" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "清除筛选" })).toHaveAttribute("href", "/library");
  });

  it("links every published game to its detail page", () => {
    const game: GameSummary = { gameId: "01980000-0000-7000-8000-000000000001", title: "Metroid", platform: { id: "gba", name: "Game Boy Advance" }, platformInstance: { id: "instance", name: "GBA 游戏" }, status: "PUBLISHED", coverUrl: null };
    render(<GameGrid games={[game]} />);
    expect(screen.getByRole("link", { name: /Metroid/ })).toHaveAttribute("href", `/games/${game.gameId}`);
    expect(screen.getByText("可运行")).toBeInTheDocument();
  });
});
