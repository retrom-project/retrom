import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { HomeDiscover } from "./home-discover";

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const games = Array.from({ length: 15 }, (_, index) => ({
  id: `game-${index}`, title: `Game ${index}`, platformId: "nes", directoryName: "NES", media: [],
}));
function respondToDiscovery() {
  const fetch = vi.fn(async (request: Request) => {
    const url = new URL(request.url);
    const limit = Number(url.searchParams.get("limit"));
    expect(url.pathname).toBe("/api/v1/games");
    expect(url.searchParams.get("sort")).toBe("random");
    return Response.json({ items: games.slice(0, limit), total: games.length, offset: 0, limit });
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

it.each([{ phone: false, count: 12 }, { phone: true, count: 6 }])(
  "shows $count randomly selected games in the corresponding layout",
  async ({ phone, count }) => {
    const fetch = respondToDiscovery();
    const view = render(<HomeDiscover phone={phone} />);
    const section = screen.getByRole("region", { name: "发现游戏" });
    await waitFor(() => expect(within(section).getAllByRole("link")).toHaveLength(count + 1));
    expect(section.querySelectorAll(phone ? ".phone-game-card" : ".home-recent-card")).toHaveLength(count);
    expect(within(section).getByRole("link", { name: "查看全部" })).toHaveAttribute("href", "/library");
    expect(within(section).getByRole("link", { name: /Game 0/u })).toHaveAttribute("href", "/games/game-0");
    view.rerender(<HomeDiscover phone={phone} />);
    expect(fetch).toHaveBeenCalledOnce();
  },
);

it("loads the desktop batch when the phone layout changes", async () => {
  const fetch = respondToDiscovery();
  const view = render(<HomeDiscover phone />);
  await waitFor(() => expect(view.container.querySelectorAll(".phone-game-card")).toHaveLength(6));
  view.rerender(<HomeDiscover />);
  await waitFor(() => expect(view.container.querySelectorAll(".home-recent-card")).toHaveLength(12));
  expect(fetch).toHaveBeenCalledTimes(2);
});

it("retries only the discovery request after a failure and shows an empty library accurately", async () => {
  const fetch = vi.fn().mockResolvedValueOnce(Response.json({ code: "FAILED", message: "读取失败" }, { status: 500 }))
    .mockResolvedValueOnce(Response.json({ items: [], total: 0, offset: 0, limit: 12 }));
  vi.stubGlobal("fetch", fetch);
  render(<HomeDiscover />);
  fireEvent.click(await screen.findByRole("button", { name: "重试" }));
  expect(await screen.findByText("游戏库中还没有可展示的游戏。")).toBeInTheDocument();
  expect(fetch).toHaveBeenCalledTimes(2);
});
