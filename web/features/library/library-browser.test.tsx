import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { filterLibraryGames, type GamePage, type GameSummary, type LibraryFilters } from "./game-library";
import { LibraryBrowser } from "./library-browser";

const auth = vi.hoisted(() => ({ fetch: vi.fn() }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ authenticatedFetch: auth.fetch }) }));

const nowMs = new Date(2026, 7, 8, 12).getTime();
const initialFilters: LibraryFilters = { query: "", platformId: "", platformInstanceId: "", sort: "RECENT_DESC" };
const games: GameSummary[] = [
  { gameId: "1943", title: "1943", platform: { id: "arcade", name: "Arcade" }, platformInstance: { id: "fbneo", name: "FBNeo 游戏" }, defaultCore: { id: "fbneo", name: "FinalBurn Neo" }, status: "PUBLISHED", coverUrl: null, createdAtMs: nowMs - 2_000, lastPlayedAtMs: nowMs - 1_000, favorite: null, tags: [{ tagId: "tag-coop", name: "双人合作" }] },
  { gameId: "doom", title: "DOOM", platform: { id: "dos", name: "MS-DOS" }, platformInstance: { id: "dos-classics", name: "DOS 经典" }, defaultCore: { id: "dosbox_pure", name: "DOSBox Pure" }, status: "PUBLISHED", coverUrl: null, createdAtMs: nowMs - 1_000, lastPlayedAtMs: null, favorite: null, tags: [{ tagId: "tag-solo", name: "单人" }] },
];
const facets = {
  totalCount: 2,
  platforms: [{ id: "arcade", name: "Arcade", count: 1 }, { id: "dos", name: "MS-DOS", count: 1 }],
  platformInstances: [{ id: "fbneo", name: "FBNeo 游戏", platformId: "arcade", count: 1 }, { id: "dos-classics", name: "DOS 经典", platformId: "dos", count: 1 }],
  tags: [{ id: "tag-coop", name: "双人合作", count: 1 }, { id: "tag-solo", name: "单人", count: 1 }],
};

function page(items = games, nextCursor: string | null = null): GamePage {
  return { generatedAtMs: nowMs, items, nextCursor, filteredCount: items.length, facets };
}

function json(data: GamePage) {
  return { ok: true, json: async () => data } as Response;
}

describe("LibraryBrowser", () => {
  beforeEach(() => {
    window.history.replaceState({}, "", "/library");
    auth.fetch.mockReset().mockImplementation(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), "http://localhost");
      const filtered = filterLibraryGames(games, {
        query: url.searchParams.get("q") ?? "",
        platformId: url.searchParams.get("platformId") ?? "",
        platformInstanceId: url.searchParams.get("platformInstanceId") ?? "",
        tagId: url.searchParams.get("tagId") ?? "",
        sort: (url.searchParams.get("sort") ?? "RECENT_DESC") as LibraryFilters["sort"],
      });
      return json(page(filtered));
    });
  });
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("preserves the server-rendered first page when development Strict Mode replays effects", async () => {
    render(<StrictMode><LibraryBrowser initialPage={page()} initialFilters={initialFilters} /></StrictMode>);
    await act(async () => { await new Promise((resolve) => window.setTimeout(resolve, 250)); });
    expect(auth.fetch).not.toHaveBeenCalled();
    expect(screen.getByRole("heading", { name: "1943" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "DOOM" })).toBeVisible();
  });

  it("filters immediately with platform counts and URL state", async () => {
    const user = userEvent.setup();
    render(<LibraryBrowser initialPage={page()} initialFilters={initialFilters} />);

    expect(screen.getByRole("button", { name: "全部 2" })).toHaveAttribute("aria-pressed", "true");
    await user.click(screen.getByRole("button", { name: "Arcade 1" }));
    await waitFor(() => expect(screen.getByRole("region", { name: "游戏筛选" })).toHaveTextContent("已加载 1 / 1 款游戏"));
    expect(screen.queryByRole("heading", { name: "DOOM" })).not.toBeInTheDocument();
    expect(window.location.search).toBe("?platformId=arcade");

    await user.type(screen.getByRole("searchbox", { name: "搜索游戏" }), "missing");
    expect(await screen.findByRole("heading", { name: "没有找到游戏" })).toBeInTheDocument();
    expect(window.location.search).toContain("q=missing");
  });

  it("keeps collection options dependent on the selected platform", async () => {
    const user = userEvent.setup();
    render(<LibraryBrowser initialPage={page()} initialFilters={initialFilters} />);
    await user.click(screen.getByRole("button", { name: "Arcade 1" }));
    const collection = screen.getByRole("combobox", { name: "游戏集合" });
    expect(collection).toHaveTextContent("FBNeo 游戏");
    expect(collection).not.toHaveTextContent("DOS 经典");
    await user.selectOptions(collection, "fbneo");
    expect(window.location.search).toContain("platformInstanceId=fbneo");
  });

  it("applies the exact tag id filter and keeps it in URL state", async () => {
    const user = userEvent.setup();
    render(<LibraryBrowser initialPage={page()} initialFilters={initialFilters} />);
    await user.selectOptions(screen.getByRole("combobox", { name: "标签" }), "tag-coop");

    expect(await screen.findByRole("heading", { name: "1943" })).toBeVisible();
    expect(screen.queryByRole("heading", { name: "DOOM" })).not.toBeInTheDocument();
    expect(window.location.search).toBe("?tagId=tag-coop");
  });

  it("focuses search with the slash shortcut outside editing controls", () => {
    render(<LibraryBrowser initialPage={page()} initialFilters={initialFilters} />);
    fireEvent.keyDown(document, { key: "/" });
    expect(screen.getByRole("searchbox", { name: "搜索游戏" })).toHaveFocus();
  });

  it("applies or discards mobile filter drafts and restores the trigger focus", async () => {
    const user = userEvent.setup();
    render(<LibraryBrowser initialPage={page()} initialFilters={initialFilters} />);
    const trigger = screen.getByRole("button", { name: "筛选与排序" });

    await user.click(trigger);
    let sheet = screen.getByRole("dialog", { name: "筛选与排序" });
    await user.selectOptions(within(sheet).getByRole("combobox", { name: "排列顺序" }), "TITLE_ASC");
    await user.click(within(sheet).getByRole("button", { name: "取消" }));
    expect(window.location.search).toBe("");
    expect(trigger).toHaveFocus();

    await user.click(trigger);
    sheet = screen.getByRole("dialog", { name: "筛选与排序" });
    await user.selectOptions(within(sheet).getByRole("combobox", { name: "排列顺序" }), "TITLE_ASC");
    await user.click(within(sheet).getByRole("button", { name: "应用" }));
    expect(window.location.search).toBe("?sort=TITLE_ASC");
    expect(screen.getByRole("button", { name: "筛选与排序 · 1" })).toHaveFocus();
  });

  it("loads one 50-item cursor page per sentinel entry and suppresses duplicate requests", async () => {
    const observers: Array<(entries: IntersectionObserverEntry[]) => void> = [];
    class IntersectionObserverMock {
      constructor(callback: IntersectionObserverCallback) {
        observers.push((entries) => callback(entries, this as unknown as IntersectionObserver));
      }
      observe() {}
      disconnect() {}
      unobserve() {}
      takeRecords() { return []; }
      readonly root = null;
      readonly rootMargin = "600px 0px";
      readonly thresholds = [0];
    }
    vi.stubGlobal("IntersectionObserver", IntersectionObserverMock);
    auth.fetch
      .mockResolvedValueOnce(json({ generatedAtMs: nowMs + 1, items: [games[1]], nextCursor: "cursor-2" }))
      .mockResolvedValueOnce(json({ generatedAtMs: nowMs + 2, items: [], nextCursor: null }));

    render(<LibraryBrowser initialPage={page([games[0]], "cursor-1")} initialFilters={initialFilters} />);
    expect(observers).toHaveLength(1);

    await act(async () => observers[0]([{ isIntersecting: true } as IntersectionObserverEntry]));
    await screen.findByRole("heading", { name: "DOOM" });
    expect(auth.fetch).toHaveBeenCalledTimes(1);
    expect(auth.fetch).toHaveBeenCalledWith(
      "/api/v1/games?limit=50&sort=RECENT_DESC&cursor=cursor-1",
      { cache: "no-store", signal: undefined },
    );

    await act(async () => observers.at(-1)?.([{ isIntersecting: true } as IntersectionObserverEntry]));
    expect(auth.fetch).toHaveBeenCalledTimes(1);

    await act(async () => {
      observers.at(-1)?.([{ isIntersecting: false } as IntersectionObserverEntry]);
      observers.at(-1)?.([{ isIntersecting: true } as IntersectionObserverEntry]);
    });
    await waitFor(() => expect(auth.fetch).toHaveBeenCalledTimes(2));
    expect(auth.fetch).toHaveBeenLastCalledWith(
      "/api/v1/games?limit=50&sort=RECENT_DESC&cursor=cursor-2",
      { cache: "no-store", signal: undefined },
    );
  });
});
