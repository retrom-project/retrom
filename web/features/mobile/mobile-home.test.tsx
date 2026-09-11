import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Home, RecentGame } from "@/features/home/home-data";
import { MobileHome } from "./mobile-home";

vi.mock("@/features/player/launch-button", () => ({ LaunchButton: ({ gameId, saveStateId, label }: { gameId: string; saveStateId?: string | null; label: string }) => <button data-game={gameId} data-save={saveStateId}>{label}</button> }));
vi.mock("@/features/home/immersive-home-entry", () => ({ ImmersiveHomeEntry: () => <button>沉浸模式</button> }));
afterEach(cleanup);

function recentGame(index: number): RecentGame {
  return { gameId: `game-${index}`, title: `游戏 ${index}`, platform: { id: "gba", name: "GBA" }, platformInstance: { id: "one", name: "目录" }, lastPlayedAtMs: 1000, activeDurationMs: 100, sessionCount: 1, coverUrl: null, tags: [] };
}

function home(): Home {
  return { library: { gameCount: 10, saveStateCount: 1 }, play: { activeDurationMs: 100 }, platforms: [], quickPlatforms: [], latestGames: [],
    featuredGame: { ...recentGame(0), description: "", hasSaveStates: false, lastSessionSave: null },
    recentGames: Array.from({ length: 10 }, (_, index) => recentGame(index)),
  };
}

describe("phone home", () => {
  it("keeps search, launch and six recent games without dashboard sections", () => {
    const data = home();
    data.latestGames = [{ ...recentGame(99), createdAtMs: 1000 }];
    const { container } = render(<MobileHome home={data} />);
    expect(screen.getByRole("search")).toHaveAttribute("action", "/library");
    expect(screen.getByRole("searchbox", { name: "搜索游戏" })).toHaveAttribute("name", "q");
    expect(screen.getByRole("button", { name: "开始游戏" })).not.toHaveAttribute("data-save");
    expect(container.querySelectorAll(".phone-game-card")).toHaveLength(6);
    expect(screen.queryByText("快速开始")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "沉浸模式" })).not.toBeInTheDocument();
    expect(screen.queryByText("累计游玩")).not.toBeInTheDocument();
    expect(screen.queryByText("游戏 9")).not.toBeInTheDocument();
    expect(screen.queryByText("游戏 99")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "查看全部" })).toHaveAttribute("href", "/recent");
  });

  it("continues the associated save instead of inferring progress from history", () => {
    const data = home();
    data.featuredGame = { ...recentGame(0), description: "", hasSaveStates: true, lastSessionSave: {
      saveStateId: "saved-progress", createdAtMs: 1000, activeDurationMs: 100,
      screenshotUrl: null, discIndex: null, discLabel: null,
    } };
    render(<MobileHome home={data} />);
    expect(screen.getByRole("button", { name: "从存档继续" })).toHaveAttribute("data-save", "saved-progress");
    expect(screen.queryByRole("button", { name: "开始游戏" })).not.toBeInTheDocument();
  });

  it("offers other library games when the featured game is the only history", () => {
    const data = home();
    data.recentGames = [recentGame(0)];
    data.latestGames = Array.from({ length: 9 }, (_, index) => ({ ...recentGame(index), createdAtMs: 1000 }));
    const { container } = render(<MobileHome home={data} />);
    expect(screen.getByRole("heading", { name: "发现游戏" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "查看全部" })).toHaveAttribute("href", "/library");
    expect(container.querySelectorAll(".phone-game-card")).toHaveLength(6);
    expect(container.querySelector('.phone-game-card[href="/games/game-0"]')).toBeNull();
    expect(screen.queryByRole("heading", { name: "最近游戏" })).not.toBeInTheDocument();
  });

  it("offers a usable library path without empty continue or recent sections", () => {
    const data = home();
    data.featuredGame = null;
    data.recentGames = [];
    data.library.gameCount = 0;
    render(<MobileHome home={data} />);
    expect(screen.getByRole("heading", { name: "游戏库还是空的" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "浏览游戏库" })).toHaveAttribute("href", "/library");
    expect(screen.queryByText("最近游戏")).not.toBeInTheDocument();
    expect(screen.queryByText("管理后台")).not.toBeInTheDocument();
  });

  it("shows a bounded library selection before the first play", () => {
    const data = home();
    data.featuredGame = null;
    data.recentGames = [];
    data.latestGames = Array.from({ length: 8 }, (_, index) => ({ ...recentGame(index), createdAtMs: 1000 }));
    const { container } = render(<MobileHome home={data} />);
    expect(screen.getByRole("heading", { name: "从这里开始" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "查看全部" })).toHaveAttribute("href", "/library");
    expect(container.querySelectorAll(".phone-game-card")).toHaveLength(6);
    expect(screen.queryByRole("button", { name: "开始游戏" })).not.toBeInTheDocument();
  });
});
