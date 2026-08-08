import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import { GameGrid, type GameSummary } from "./game-grid";

afterEach(cleanup);

const game: GameSummary = {
  gameId: "01980000-0000-7000-8000-000000000001",
  title: "Metroid",
  platform: { id: "gba", name: "Game Boy Advance" },
  platformInstance: { id: "instance", name: "GBA 游戏" },
  defaultCore: { id: "mgba", name: "mGBA" },
  status: "PUBLISHED",
  coverUrl: "/content/assets/cover",
  createdAtMs: new Date(2026, 7, 7).getTime(),
  lastPlayedAtMs: new Date(2026, 7, 8, 1, 20).getTime(),
};

describe("GameGrid", () => {
  it("renders an actionable empty state", () => {
    render(<GameGrid games={[]} nowMs={new Date(2026, 7, 8, 12).getTime()} />);
    expect(screen.getByRole("heading", { name: "游戏库还是空的" })).toBeInTheDocument();
  });

  it("distinguishes an empty filtered result from an empty library", () => {
    render(<GameGrid games={[]} nowMs={new Date(2026, 7, 8, 12).getTime()} filtered />);
    expect(screen.getByRole("heading", { name: "没有找到游戏" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "清除筛选" })).toHaveAttribute("href", "/library");
  });

  it("links the cover and title to detail without repeating normal status", () => {
    render(<GameGrid games={[game]} nowMs={new Date(2026, 7, 8, 12).getTime()} />);
    expect(screen.getAllByRole("link", { name: /Metroid/ })).toHaveLength(2);
    expect(screen.getByRole("img", { name: "Metroid 封面" }).getAttribute("src")).toContain("cover");
    expect(screen.getByText("今天 01:20")).toBeInTheDocument();
    expect(screen.queryByText("可运行")).not.toBeInTheDocument();
  });

  it("uses an identifiable local poster when the cover is missing", () => {
    render(<GameGrid games={[{ ...game, coverUrl: null }]} nowMs={new Date(2026, 7, 8, 12).getTime()} />);
    expect(screen.getByLabelText("Metroid 暂无封面")).toHaveTextContent("RETROM CLASSICS");
    expect(screen.getByLabelText("Metroid 暂无封面")).toHaveTextContent("mGBA");
  });

  it("offers useful card actions from the compact menu", async () => {
    const user = userEvent.setup();
    render(<GameGrid games={[game]} nowMs={new Date(2026, 7, 8, 12).getTime()} />);
    await user.click(screen.getByRole("button", { name: "游戏“Metroid”的更多操作" }));
    expect(screen.getByRole("menuitem", { name: "查看游戏详情" })).toHaveAttribute("href", `/games/${game.gameId}`);
    expect(screen.getByRole("menuitem", { name: "查看相关存档" })).toHaveAttribute("href", `/saves?gameId=${game.gameId}`);
  });

  it("only shows a semantic status when a game needs attention", () => {
    render(<GameGrid games={[{ ...game, status: "DELETED", coverUrl: null }]} nowMs={new Date(2026, 7, 8, 12).getTime()} />);
    expect(screen.getByLabelText("游戏当前不可见")).toBeInTheDocument();
  });
});
