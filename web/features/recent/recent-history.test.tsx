import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RecentHistory } from "./recent-history";
import type { RecentGame } from "./recent-games";

vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: vi.fn() }) }));

afterEach(cleanup);

const nowMs = new Date(2026, 7, 8, 12, 0).getTime();
const games: RecentGame[] = [
  {
    gameId: "today",
    title: "1943: The Battle of Midway",
    status: "PUBLISHED",
    availability: "PUBLISHED",
    platform: { id: "arcade", name: "街机" },
    platformInstance: { id: "fbneo", name: "FBNeo 游戏" },
    lastPlayedAtMs: new Date(2026, 7, 8, 1, 20).getTime(),
    activeDurationMs: 40_000,
    sessionCount: 1,
    coverUrl: null,
  },
  {
    gameId: "older",
    title: "Pokémon Green",
    status: "PUBLISHED",
    availability: "PUBLISHED",
    platform: { id: "gbc", name: "Game Boy / Color" },
    platformInstance: { id: "handheld", name: "掌机收藏" },
    lastPlayedAtMs: nowMs - 40 * 86_400_000,
    activeDurationMs: 300_000,
    sessionCount: 4,
    coverUrl: null,
  },
];

describe("recent history", () => {
  it("renders summaries and keeps direct launch and detail actions separate", () => {
    render(<RecentHistory games={games} nowMs={nowMs} />);
    const summary = screen.getByRole("region", { name: "游玩统计" });
    expect(within(summary).getByText("2")).toBeVisible();
    expect(screen.getByRole("heading", { name: "今天" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "更早" })).toBeVisible();
    expect(screen.getAllByRole("button", { name: "再玩一次" })).toHaveLength(2);
    expect(screen.getAllByRole("link", { name: /^查看详情/ })).toHaveLength(2);
  });

  it("updates the result in place for query, platform and rolling time filters", async () => {
    const user = userEvent.setup();
    render(<RecentHistory games={games} nowMs={nowMs} />);
    await user.type(screen.getByRole("searchbox", { name: "搜索游戏" }), "掌机收藏");
    expect(screen.getByText("Pokémon Green")).toBeVisible();
    expect(screen.queryByText("1943: The Battle of Midway")).not.toBeInTheDocument();
    await user.clear(screen.getByRole("searchbox", { name: "搜索游戏" }));
    await user.selectOptions(screen.getByRole("combobox", { name: "游戏平台" }), "arcade");
    expect(screen.getByText("1943: The Battle of Midway")).toBeVisible();
    expect(screen.queryByText("Pokémon Green")).not.toBeInTheDocument();
    await user.selectOptions(screen.getByRole("combobox", { name: "游戏平台" }), "");
    await user.click(screen.getByRole("button", { name: "7 天" }));
    expect(screen.getByText("1943: The Battle of Midway")).toBeVisible();
    expect(screen.queryByText("Pokémon Green")).not.toBeInTheDocument();
    expect(document.querySelector(".recent-result-count")).toHaveTextContent("共 1 款");
  });

  it("keeps a deleted game as a text tombstone without payload or executable actions", () => {
    const deleted = { ...games[0], status: "DELETED" as const, availability: "DELETED" as const, coverUrl: "/legacy-cover.png" };
    render(<RecentHistory games={[deleted, games[1]]} nowMs={nowMs} />);

    expect(screen.getByText("已删除")).toBeVisible();
    expect(screen.getByText("已删除游戏")).toBeVisible();
    expect(screen.queryByRole("link", { name: /1943: The Battle of Midway/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("img", { name: /1943: The Battle of Midway/ })).not.toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "再玩一次" })).toHaveLength(1);
  });
});
