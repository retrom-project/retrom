import { cleanup, render, screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AdminGameBrowser } from "./admin-game-browser";
import type { AdminGameFilters, AdminGameSummary } from "./admin-game-library";

const { fetchPage } = vi.hoisted(() => ({ fetchPage: vi.fn() }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ authenticatedFetch: fetchPage }) }));

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

function initialPage(items: AdminGameSummary[], nextCursor: string | null = null, total = items.length) {
  return { items, generatedAtMs: 500, nextCursor, filteredCount: total,
    summary: { total, runtimeAttention: 0, missingCover: total, incompleteMetadata: 0, hidden: 1 },
    facets: { totalCount: total, platforms: [{ id: "arcade", name: "Arcade", count: total }, { id: "nes", name: "NES", count: 1 }],
      platformInstances: [{ id: "fbneo", name: "FBNeo 游戏", platformId: "arcade", count: total }, { id: "nes-main", name: "NES 游戏", platformId: "nes", count: 1 }],
      tags: [{ id: "tag", name: "掌机精选", count: 1 }] },
  };
}

describe("AdminGameBrowser", () => {
  beforeEach(() => { window.history.replaceState({ marker: "keep" }, "", "/admin/games"); fetchPage.mockReset(); });
  afterEach(cleanup);

  it("renders only the initial page with global stats and tags without a full-library request", () => {
    const { container } = render(<AdminGameBrowser initialPage={initialPage([game(1)], "next", 3625)} initialFilters={filters} />);
    const row = screen.getByRole("link", { name: "Game 1" }).closest("tr")!;
    expect(within(row).queryByText("掌机精选")).not.toBeInTheDocument();
    expect(container.querySelector(".admin-game-identity .tag-chips")).toBeNull();
    expect(screen.getAllByText("3625")).toHaveLength(2);
    expect(screen.getByRole("option", { name: "掌机精选 · 1" })).toBeInTheDocument();
    expect(fetchPage).not.toHaveBeenCalled();
  });

  it("filters the server dataset in place and preserves URL history state", async () => {
    fetchPage.mockResolvedValue({ ok: true, json: async () => ({ ...initialPage([game(2, { title: "Metal Slug" })]), filteredCount: 1 }) });
    const user = userEvent.setup();
    render(<AdminGameBrowser initialPage={initialPage([game(1)])} initialFilters={filters} />);
    await user.type(screen.getByRole("searchbox", { name: "搜索游戏" }), "metal");
    expect(await screen.findByRole("link", { name: "Metal Slug" })).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("link", { name: "Game 1" })).not.toBeInTheDocument());
    expect(fetchPage.mock.calls.at(-1)?.[0]).toContain("q=metal");
    expect(window.location.search).toBe("?q=metal");
    expect(window.history.state).toEqual({ marker: "keep" });
  });

  it("replaces six rows with the next server page without downloading later pages", async () => {
    const user = userEvent.setup();
    fetchPage.mockResolvedValue({ ok: true, json: async () => ({ generatedAtMs: 501, items: [game(7)], nextCursor: null }) });
    render(<AdminGameBrowser initialPage={initialPage(Array.from({ length: 6 }, (_, index) => game(index + 1)), "next", 7)} initialFilters={filters} />);
    expect(within(screen.getByRole("table")).getAllByRole("row")).toHaveLength(7);
    await user.click(screen.getByRole("button", { name: "下一页" }));
    await waitFor(() => expect(screen.getByText("第 2 页 · 共 2 页")).toBeInTheDocument());
    expect(within(screen.getByRole("table")).getAllByRole("row")).toHaveLength(2);
    expect(fetchPage).toHaveBeenCalledTimes(1);
    expect(fetchPage.mock.calls[0][0]).toContain("cursor=next");
  });

  it("focuses search with slash and uses complete directory facets", async () => {
    const user = userEvent.setup();
    fetchPage.mockResolvedValue({ ok: true, json: async () => initialPage([]) });
    render(<AdminGameBrowser initialPage={initialPage([game(1)])} initialFilters={filters} />);
    await user.keyboard("/");
    expect(screen.getByRole("searchbox", { name: "搜索游戏" })).toHaveFocus();
    await user.selectOptions(screen.getByRole("combobox", { name: "平台" }), "nes");
    expect(within(screen.getByRole("combobox", { name: "游戏目录" })).getByRole("option", { name: "NES 游戏" })).toBeInTheDocument();
    expect(within(screen.getByRole("combobox", { name: "游戏目录" })).queryByRole("option", { name: "FBNeo 游戏" })).not.toBeInTheDocument();
  });

  it("keeps deleted status and delegates its filter to the server", async () => {
    const deleted = game(2, { status: "DELETED", runtimeStatus: "READY" });
    fetchPage.mockResolvedValue({ ok: true, json: async () => initialPage([deleted]) });
    const user = userEvent.setup();
    render(<AdminGameBrowser initialPage={initialPage([game(1), deleted])} initialFilters={filters} />);
    const row = screen.getByRole("link", { name: "Game 2" }).closest("tr")!;
    expect(within(row).getByText("已删除")).toBeInTheDocument();
    expect(within(row).queryByText("可以运行")).not.toBeInTheDocument();
    await user.selectOptions(screen.getByRole("combobox", { name: "运行状态" }), "DELETED");
    expect(await screen.findByRole("link", { name: "Game 2" })).toBeInTheDocument();
    await waitFor(() => expect(fetchPage.mock.calls.at(-1)?.[0]).toContain("runtime=DELETED"));
    await waitFor(() => expect(screen.queryByRole("link", { name: "Game 1" })).not.toBeInTheDocument());
  });
});
