import type { ReactNode } from "react";
import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import HomePage from "@/app/page";
import type { Home, FeaturedGame } from "./home-data";

vi.mock("./home-favorites", () => ({ HomeFavorites: () => null }));

const mocks = vi.hoisted(() => ({ backendJSON: vi.fn() }));
vi.mock("@/lib/server-backend", () => ({ backendJSON: mocks.backendJSON }));
vi.mock("@/features/mobile/phone-layout", () => ({ PhoneLayout: ({ children }: { children: ReactNode }) => children }));
vi.mock("@/features/immersive/entry-dialog", () => ({ ImmersiveEntryDialog: () => null }));
vi.mock("./immersive-home-entry", () => ({ ImmersiveHomeEntry: () => null }));
vi.mock("@/features/player/launch-button", () => ({ LaunchButton: ({ gameId, saveStateId, dosEntry, returnTo, label }: { gameId: string; saveStateId?: string | null; dosEntry?: string | null; returnTo: string; label: string }) => <button data-game={gameId} data-save={saveStateId} data-dos-entry={dosEntry} data-return={returnTo}>{label}</button> }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: { user: { userId: "home-test" } } }) }));
afterEach(cleanup);

function featured(): FeaturedGame {
  return { gameId: "game-1", title: "冒险游戏", platform: { id: "gba", name: "GBA" }, platformInstance: { id: "one", name: "掌机游戏" }, lastPlayedAtMs: 1000, activeDurationMs: 60000, sessionCount: 2, coverUrl: null, tags: [], description: "", hasSaveStates: false, defaultDosEntry: null, lastSessionSave: null };
}

async function showHome(game: FeaturedGame | null) {
  const home: Home = { library: { gameCount: game ? 1 : 0, saveStateCount: game?.hasSaveStates ? 1 : 0 }, play: { activeDurationMs: 60000 }, featuredGame: game, recentGames: [], latestGames: [], platforms: [], quickPlatforms: [] };
  mocks.backendJSON.mockResolvedValue(home);
  const result = render(await HomePage());
  const panel = result.container.querySelector<HTMLElement>(".home-featured-panel");
  if (!panel) {throw new Error("Missing featured panel");}
  return within(panel);
}

describe("desktop home", () => {
  it("keeps the featured game focused on title and launch instead of duplicating its description", async () => {
    const panel = await showHome({ ...featured(), description: "游戏简介".repeat(1000) });
    expect(panel.getByRole("heading", { name: "冒险游戏" })).toBeInTheDocument();
    expect(panel.queryByText(/游戏简介/)).not.toBeInTheDocument();
    expect(panel.getByRole("button", { name: "再玩一次" })).toBeInTheDocument();
  });

  it("offers a library action when there is no play history", async () => {
    const panel = await showHome(null);
    expect(panel.getByRole("link", { name: "浏览游戏库" })).toHaveAttribute("href", "/library");
    expect(panel.getByRole("heading", { name: "收藏，从第一款开始。" })).toBeInTheDocument();
    expect(panel.queryByRole("button")).not.toBeInTheDocument();
  });

  it("keeps old saves discoverable without treating them as the last session progress", async () => {
    const panel = await showHome({ ...featured(), description: "", hasSaveStates: true });
    expect(panel.getAllByText("冒险游戏", { exact: true })).toHaveLength(1);
    expect(panel.getByRole("link", { name: "查看存档" })).toHaveAttribute("href", "/saves?gameId=game-1");
    expect(panel.getByRole("button", { name: "再玩一次" })).not.toHaveAttribute("data-save");
    expect(panel.getByRole("button", { name: "再玩一次" })).toHaveAttribute("data-return", "/");
    expect(panel.getByText("本次将从游戏开头启动")).toBeInTheDocument();
    expect(panel.getByRole("link", { name: "查看游戏详情" })).toHaveAttribute("href", "/games/game-1");
  });

  it("starts a DOS game from its reviewed default program without a last-session save", async () => {
    const panel = await showHome({ ...featured(), platform: { id: "dos", name: "MS-DOS" }, defaultDosEntry: "PAL/PLAY.BAT", hasSaveStates: true });
    const launch = panel.getByRole("button", { name: "再玩一次" });
    expect(launch).toHaveAttribute("data-dos-entry", "PAL/PLAY.BAT");
    expect(launch).not.toHaveAttribute("data-save");
  });

  it("continues the exact session save without requiring a preview and keeps the launch explanation", async () => {
    const panel = await showHome({ ...featured(), description: "", defaultDosEntry: "PAL/PLAY.BAT", hasSaveStates: true, lastSessionSave: { saveStateId: "save-1", createdAtMs: 1000, activeDurationMs: 60000, screenshotUrl: null, discIndex: 1, discLabel: "光盘 2" } });
    expect(panel.getByRole("button", { name: "从存档继续" })).toHaveAttribute("data-save", "save-1");
    expect(panel.getByRole("button", { name: "从存档继续" })).not.toHaveAttribute("data-dos-entry");
    expect(panel.getByText("本次将从存档位置启动")).toBeInTheDocument();
    expect(panel.getByText(/手动存档.*光盘 2/)).toBeInTheDocument();
    expect(screen.queryByText("暂无可恢复存档")).not.toBeInTheDocument();
  });
});

it("deduplicates the featured game from recent posters and uses latest games only before the first play", async () => {
  const current = featured();
  const other = { ...current, gameId: "other", title: "另一款游戏" };
  const data: Home = { library: { gameCount: 2, saveStateCount: 0 }, play: { activeDurationMs: 0 }, featuredGame: current, recentGames: [current, other], latestGames: [{ ...other, createdAtMs: 1000 }], platforms: [], quickPlatforms: [] };
  mocks.backendJSON.mockResolvedValue(data);
  const view = render(await HomePage());
  expect(view.container.querySelectorAll(".home-recent-card")).toHaveLength(1);
  expect(view.container.querySelector(".home-recent-card")).toHaveAttribute("href", "/games/other");
  cleanup();
  mocks.backendJSON.mockResolvedValue({ ...data, featuredGame: null, recentGames: [] });
  render(await HomePage());
  expect(screen.getByRole("heading", { name: "从这里开始" })).toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "最新添加" })).not.toBeInTheDocument();
});
