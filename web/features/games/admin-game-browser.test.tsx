import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { AdminGameBrowser } from "./admin-game-browser";
import type { AdminGameFilters, AdminGameSummary } from "./admin-game-library";

const filters: AdminGameFilters = { query: "", platformId: "", platformInstanceId: "", visibility: "ALL", runtime: "ALL", sort: "UPDATED_DESC" };

function game(index: number, overrides: Partial<AdminGameSummary> = {}): AdminGameSummary {
  return {
    gameId: `game-${index}`,
    title: `Game ${index}`,
    platform: { id: "arcade", name: "Arcade" },
    platformInstance: { id: "fbneo", name: "FBNeo 游戏" },
    defaultCore: { id: "fbneo", name: "FinalBurn Neo" },
    status: "PUBLISHED",
    coverUrl: null,
    createdAtMs: index,
    lastPlayedAtMs: null,
    favorite: null,
    version: 1,
    updatedAtMs: index,
    releaseYear: 1990 + index,
    metadataComplete: true,
    runtimeStatus: "READY",
    ...overrides,
  };
}

describe("AdminGameBrowser", () => {
  beforeEach(() => window.history.replaceState({ marker: "keep" }, "", "/admin/games"));
  afterEach(cleanup);

  it("filters immediately without replacing the document and preserves URL history state", async () => {
    const user = userEvent.setup();
    render(<AdminGameBrowser games={[game(1), game(2, { title: "Metal Slug" })]} nowMs={500} initialFilters={filters} />);
    await user.type(screen.getByRole("searchbox", { name: "搜索游戏" }), "metal");
    expect(screen.getByRole("link", { name: "Metal Slug" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Game 1" })).not.toBeInTheDocument();
    expect(window.location.search).toBe("?q=metal");
    expect(window.history.state).toEqual({ marker: "keep" });
  });

  it("limits each page to six games and supports in-place pagination", async () => {
    const user = userEvent.setup();
    render(<AdminGameBrowser games={Array.from({ length: 7 }, (_, index) => game(index + 1))} nowMs={500} initialFilters={filters} />);
    expect(within(screen.getByRole("table")).getAllByRole("row")).toHaveLength(7);
    await user.click(screen.getByRole("button", { name: "下一页" }));
    expect(within(screen.getByRole("table")).getAllByRole("row")).toHaveLength(2);
    expect(screen.getByText("第 2 页 · 共 2 页")).toBeInTheDocument();
  });

  it("focuses search with slash and narrows directories after platform changes", async () => {
    const user = userEvent.setup();
    render(<AdminGameBrowser games={[game(1), game(2, { platform: { id: "nes", name: "NES" }, platformInstance: { id: "nes-main", name: "NES 游戏" } })]} nowMs={500} initialFilters={filters} />);
    await user.keyboard("/");
    expect(screen.getByRole("searchbox", { name: "搜索游戏" })).toHaveFocus();
    await user.selectOptions(screen.getByRole("combobox", { name: "平台" }), "nes");
    expect(within(screen.getByRole("combobox", { name: "游戏目录" })).queryByRole("option", { name: "FBNeo 游戏" })).not.toBeInTheDocument();
  });
});
