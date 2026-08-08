import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { GameSummary, LibraryFilters } from "./game-library";
import { LibraryBrowser } from "./library-browser";

const nowMs = new Date(2026, 7, 8, 12).getTime();
const initialFilters: LibraryFilters = { query: "", platformId: "", platformInstanceId: "", sort: "RECENT_DESC" };
const games: GameSummary[] = [
  { gameId: "1943", title: "1943", platform: { id: "arcade", name: "Arcade" }, platformInstance: { id: "fbneo", name: "FBNeo 游戏" }, defaultCore: { id: "fbneo", name: "FinalBurn Neo" }, status: "PUBLISHED", coverUrl: null, createdAtMs: nowMs - 2_000, lastPlayedAtMs: nowMs - 1_000 },
  { gameId: "doom", title: "DOOM", platform: { id: "dos", name: "MS-DOS" }, platformInstance: { id: "dos-classics", name: "DOS 经典" }, defaultCore: { id: "dosbox_pure", name: "DOSBox Pure" }, status: "PUBLISHED", coverUrl: null, createdAtMs: nowMs - 1_000, lastPlayedAtMs: null },
];

describe("LibraryBrowser", () => {
  beforeEach(() => window.history.replaceState({}, "", "/library"));
  afterEach(cleanup);

  it("filters immediately with platform counts and URL state", async () => {
    const user = userEvent.setup();
    render(<LibraryBrowser games={games} nowMs={nowMs} initialFilters={initialFilters} />);

    expect(screen.getByRole("button", { name: "全部 2" })).toHaveAttribute("aria-pressed", "true");
    await user.click(screen.getByRole("button", { name: "Arcade 1" }));
    expect(screen.getByRole("region", { name: "游戏筛选" })).toHaveTextContent("当前显示 1 款游戏");
    expect(screen.queryByRole("heading", { name: "DOOM" })).not.toBeInTheDocument();
    expect(window.location.search).toBe("?platformId=arcade");

    await user.type(screen.getByRole("searchbox", { name: "搜索游戏" }), "missing");
    expect(screen.getByRole("heading", { name: "没有找到游戏" })).toBeInTheDocument();
    expect(window.location.search).toContain("q=missing");
  });

  it("keeps collection options dependent on the selected platform", async () => {
    const user = userEvent.setup();
    render(<LibraryBrowser games={games} nowMs={nowMs} initialFilters={initialFilters} />);
    await user.click(screen.getByRole("button", { name: "Arcade 1" }));
    const collection = screen.getByRole("combobox", { name: "游戏集合" });
    expect(collection).toHaveTextContent("FBNeo 游戏");
    expect(collection).not.toHaveTextContent("DOS 经典");
    await user.selectOptions(collection, "fbneo");
    expect(window.location.search).toContain("platformInstanceId=fbneo");
  });

  it("focuses search with the slash shortcut outside editing controls", () => {
    render(<LibraryBrowser games={games} nowMs={nowMs} initialFilters={initialFilters} />);
    fireEvent.keyDown(document, { key: "/" });
    expect(screen.getByRole("searchbox", { name: "搜索游戏" })).toHaveFocus();
  });
});
