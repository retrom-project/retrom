import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FavoritePage } from "./favorite-api";
import { FavoriteBrowser } from "./favorite-browser";
import type { FavoriteQuery } from "./favorite-state";

const auth = vi.hoisted(() => ({ fetch: vi.fn() }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ authenticatedFetch: auth.fetch }) }));

const folderId = "01980000-0000-7000-8000-000000000011";
const query: FavoriteQuery = { scope: "ALL", folderId: "", q: "", platformId: "", sort: "FAVORITED_DESC" };

function page(overrides: Partial<FavoritePage> = {}): FavoritePage {
  return {
    generatedAtMs: 1000,
    summary: { favoriteCount: 2, uncategorizedCount: 0, folderCount: 1 },
    folders: [{ folderId, name: "想玩", version: 1, visibleGameCount: 2, createdAtMs: 1000, updatedAtMs: 1000 }],
    platforms: [{ id: "gba", name: "Game Boy Advance", count: 2 }],
    totalCount: 2,
    items: [1, 2].map((index) => ({
      gameId: `01980000-0000-7000-8000-00000000001${index}`,
      title: `Game ${index}`,
      platform: { id: "gba", name: "Game Boy Advance" },
      platformInstance: { id: "instance", name: "GBA 游戏" },
      defaultCore: { id: "mgba", name: "mGBA" },
      coverUrl: null, releaseYear: 2000 + index, createdAtMs: 1000, lastPlayedAtMs: null,
      tags: [],
      favorite: { favoritedAtMs: 1000 + index, folderIds: [folderId] },
    })),
    nextCursor: null,
    ...overrides,
  };
}

function json(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
}

describe("FavoriteBrowser", () => {
  beforeEach(() => { auth.fetch.mockReset(); window.history.replaceState({ keep: true }, "", "/favorites"); });
  afterEach(cleanup);

  it("renders complete, scope-empty, filtered-empty and retryable error states", async () => {
    const empty = page({ summary: { favoriteCount: 0, uncategorizedCount: 0, folderCount: 0 }, folders: [], platforms: [], totalCount: 0, items: [] });
    const emptyView = render(<FavoriteBrowser initialPage={empty} initialQuery={query} />);
    expect(screen.getByRole("heading", { name: "还没有收藏游戏" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "前往游戏库" })).toHaveAttribute("href", "/library");
    emptyView.unmount();

    const filteredView = render(<FavoriteBrowser initialPage={page({ totalCount: 0, items: [] })} initialQuery={{ ...query, q: "missing" }} />);
    expect(screen.getByRole("heading", { name: "没有匹配的收藏" })).toBeInTheDocument();
    filteredView.unmount();

    auth.fetch.mockResolvedValue(json(page()));
    render(<FavoriteBrowser initialPage={null} initialQuery={query} initialError="收藏读取失败" />);
    expect(screen.getByRole("alert")).toHaveTextContent("收藏读取失败");
    await userEvent.click(screen.getByRole("button", { name: "重试" }));
    await waitFor(() => expect(screen.getByRole("heading", { name: "Game 1" })).toBeInTheDocument());
  });

  it("persists URL filters and exposes folder-specific batch removal", async () => {
    const folderQuery: FavoriteQuery = { ...query, scope: "FOLDER", folderId };
    auth.fetch.mockImplementation(async (input: RequestInfo | URL) => {
      if (String(input).endsWith("/organize")) {return json({ items: [] });}
      return json(page());
    });
    const user = userEvent.setup();
    render(<FavoriteBrowser initialPage={page()} initialQuery={folderQuery} />);
    await user.selectOptions(screen.getByRole("combobox", { name: "排序方式" }), "TITLE_ASC");
    await waitFor(() => expect(window.location.search).toContain("sort=TITLE_ASC"));
    await user.click(screen.getByRole("button", { name: "批量整理" }));
    await user.click(screen.getByRole("button", { name: "选择游戏“Game 1”" }));
    expect(screen.getByRole("status", { name: "" })).toHaveTextContent("已选择 1 款");
    await user.click(screen.getByRole("button", { name: "从当前收藏夹移除" }));
    await waitFor(() => expect(auth.fetch.mock.calls.some(([input]) => String(input).endsWith("/organize"))).toBe(true));
    const organize = auth.fetch.mock.calls.find(([input]) => String(input).endsWith("/organize"));
    expect(JSON.parse(String(organize?.[1]?.body))).toEqual({ gameIds: ["01980000-0000-7000-8000-000000000011"], addFolderIds: [], removeFolderIds: [folderId] });
  });

  it("opens the uncategorized scope directly in organize mode", async () => {
    auth.fetch.mockResolvedValue(json(page({ summary: { favoriteCount: 2, uncategorizedCount: 1, folderCount: 1 } })));
    const user = userEvent.setup();
    render(<FavoriteBrowser initialPage={page({ summary: { favoriteCount: 2, uncategorizedCount: 1, folderCount: 1 } })} initialQuery={query} />);

    expect(screen.getByText(/当前显示/)).toHaveTextContent("当前显示 2 款");
    await user.click(screen.getByRole("button", { name: "整理未分类游戏" }));

    await waitFor(() => expect(window.location.search).toContain("scope=UNCATEGORIZED"));
    expect(screen.getByRole("button", { name: "完成整理" })).toBePressed();
  });

  it("keeps a failed folder deletion visible and retryable in the confirmation dialog", async () => {
    auth.fetch.mockImplementation(async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "DELETE") {return json({ error: { code: "RESOURCE_VERSION_CONFLICT", message: "收藏夹已被修改" } }, 412);}
      return json(page());
    });
    const user = userEvent.setup();
    render(<FavoriteBrowser initialPage={page()} initialQuery={{ ...query, scope: "FOLDER", folderId }} />);

    await user.click(screen.getByRole("button", { name: "编辑收藏夹" }));
    await user.click(screen.getByRole("button", { name: "删除收藏夹…" }));
    const confirmation = screen.getByRole("alertdialog", { name: "删除“想玩”？" });
    await user.click(within(confirmation).getByRole("button", { name: "删除收藏夹" }));

    expect(await within(confirmation).findByRole("alert")).toHaveTextContent("已刷新真实版本");
    expect(confirmation).toBeInTheDocument();
  });
});
