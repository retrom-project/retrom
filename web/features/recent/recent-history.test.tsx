import { act } from "react";
import { hydrateRoot, type Root } from "react-dom/client";
import { renderToString } from "react-dom/server";
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

  it("hydrates the UTC grouping before switching to the browser calendar day", async () => {
    const originalTimeZone = process.env.TZ;
    const now = Date.parse("2026-09-03T00:30:00.000Z");
    const game = {
      ...games[0],
      status: "DELETED" as const,
      availability: "DELETED" as const,
      lastPlayedAtMs: Date.parse("2026-09-02T23:30:00.000Z"),
    };
    process.env.TZ = "UTC";
    const container = document.createElement("div");
    container.innerHTML = renderToString(<RecentHistory games={[game]} nowMs={now} />);
    document.body.append(container);
    expect(container).toHaveTextContent("更早");

    process.env.TZ = "Asia/Shanghai";
    const recoverableErrors: unknown[] = [];
    let root: Root | undefined;
    await act(async () => {
      root = hydrateRoot(container, <RecentHistory games={[game]} nowMs={now} />, {
        onRecoverableError: (error) => recoverableErrors.push(error),
      });
    });

    expect(container).toHaveTextContent("今天");
    expect(container).not.toHaveTextContent("更早");
    expect(recoverableErrors).toEqual([]);
    await act(async () => root?.unmount());
    process.env.TZ = originalTimeZone;
  });
});
