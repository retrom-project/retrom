import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { HomeFavorites, loadHomeFavorites } from "./home-favorites";

const mocks = vi.hoisted(() => ({ fetch: vi.fn(), userId: "user-1" }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: { user: { userId: mocks.userId } }, authenticatedFetch: mocks.fetch }) }));
afterEach(cleanup);
beforeEach(() => {mocks.fetch.mockReset(); mocks.userId = "user-1";});

function game(id: string, availability = "PUBLISHED", coverUrl: string | null = null) {
  return { gameId: id, title: `游戏 ${id}`, availability, coverUrl, platform: { id: "gba", name: "GBA" } };
}
function response(items: ReturnType<typeof game>[], nextCursor: string | null = null) {
  return new Response(JSON.stringify({ items, nextCursor }), { status: 200 });
}

describe("home favorites", () => {
  it("requests the latest three visible favorites in server order without loading more", async () => {
    mocks.fetch.mockResolvedValue(response([game("3", "PUBLISHED", "/cover.png"), game("2"), game("1")], "more"));
    render(<HomeFavorites />);
    expect(await screen.findByRole("link", { name: "游戏 3 · GBA" })).toHaveAttribute("href", "/games/3");
    expect(screen.getAllByRole("link").slice(1).map((link) => link.textContent)).toEqual(["游戏 3GBA", "R游戏 2GBA", "R游戏 1GBA"]);
    expect(screen.getByRole("link", { name: "查看全部" })).toHaveAttribute("href", "/favorites");
    expect(mocks.fetch).toHaveBeenCalledOnce();
    expect(mocks.fetch.mock.calls[0][0]).toBe("/api/v1/favorites?scope=ALL&sort=FAVORITED_DESC&limit=3");
  });

  it("skips deleted records and follows the cursor only until three games are collected", async () => {
    mocks.fetch.mockResolvedValueOnce(response([game("deleted", "DELETED"), game("4")], "older"));
    mocks.fetch.mockResolvedValueOnce(response([game("3"), game("2"), game("1")], "unused"));
    const games = await loadHomeFavorites(mocks.fetch, new AbortController().signal);
    expect(games.map((item) => item.gameId)).toEqual(["4", "3", "2"]);
    expect(mocks.fetch).toHaveBeenCalledTimes(2);
    expect(mocks.fetch.mock.calls[1][0]).toContain("cursor=older");
  });

  it("shows fewer than three favorites without placeholders", async () => {
    mocks.fetch.mockResolvedValue(response([game("only")]));
    const { container } = render(<HomeFavorites />);
    await screen.findByRole("link", { name: "游戏 only · GBA" });
    expect(container.querySelectorAll(".home-favorite-game")).toHaveLength(1);
  });

  it("guides an empty library of favorites to the game library", async () => {
    mocks.fetch.mockResolvedValue(response([]));
    const { container } = render(<HomeFavorites />);
    expect(await screen.findByText("把喜欢的游戏留在这里")).toBeVisible();
    expect(screen.getByRole("link", { name: "浏览游戏库" })).toHaveAttribute("href", "/library");
    expect(screen.queryByText("查看全部")).not.toBeInTheDocument();
    expect(container.querySelectorAll(".home-favorite-game")).toHaveLength(0);
  });

  it("keeps failures distinct from empty favorites and supports retry", async () => {
    mocks.fetch.mockResolvedValueOnce(new Response("{}", { status: 503 }));
    mocks.fetch.mockResolvedValueOnce(response([game("retried")]));
    render(<HomeFavorites />);
    expect(await screen.findByRole("alert")).toHaveTextContent("暂时无法读取收藏");
    expect(screen.queryByText("把喜欢的游戏留在这里")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "重新加载" }));
    await screen.findByRole("link", { name: "游戏 retried · GBA" });
  });

  it("does not show a previous user's late response after the account changes", async () => {
    let resolveOld!: (value: Response) => void;
    mocks.fetch.mockImplementationOnce(() => new Promise<Response>((resolve) => {resolveOld = resolve;}));
    mocks.fetch.mockResolvedValueOnce(response([game("new-user")]));
    const { rerender } = render(<HomeFavorites />);
    mocks.userId = "user-2";
    rerender(<HomeFavorites />);
    await screen.findByRole("link", { name: "游戏 new-user · GBA" });
    resolveOld(response([game("old-user")]));
    await waitFor(() => expect(mocks.fetch.mock.calls[0][1].signal.aborted).toBe(true));
    expect(screen.queryByText("游戏 old-user")).not.toBeInTheDocument();
  });

  it("rejects repeated cursors instead of requesting indefinitely", async () => {
    mocks.fetch.mockImplementation(async () => response([game("deleted", "DELETED")], "same"));
    await expect(loadHomeFavorites(mocks.fetch, new AbortController().signal)).rejects.toThrow("Repeated favorite cursor");
    expect(mocks.fetch).toHaveBeenCalledTimes(2);
  });
});
