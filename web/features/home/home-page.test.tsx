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
vi.mock("@/features/player/launch-button", () => ({ LaunchButton: ({ gameId, saveStateId, returnTo, label }: { gameId: string; saveStateId?: string | null; returnTo: string; label: string }) => <button data-game={gameId} data-save={saveStateId} data-return={returnTo}>{label}</button> }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: { user: { userId: "home-test" } } }) }));
afterEach(cleanup);

function featured(): FeaturedGame {
  return { gameId: "game-1", title: "冒险游戏", platform: { id: "gba", name: "GBA" }, platformInstance: { id: "one", name: "掌机游戏" }, lastPlayedAtMs: 1000, activeDurationMs: 60000, sessionCount: 2, coverUrl: null, tags: [], description: "", hasSaveStates: false, lastSessionSave: null };
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
  it("shows a bounded game description between the facts and launch controls", async () => {
    const panel = await showHome({ ...featured(), description: "游戏简介".repeat(1000) });
    const description = panel.getByText("游戏简介".repeat(39) + "游...");
    expect(Array.from(description.textContent ?? "")).toHaveLength(160);
    expect(description.parentElement).toHaveClass("home-featured-description");
  });

  it("offers a library action when there is no play history", async () => {
    const panel = await showHome(null);
    expect(panel.getByRole("link", { name: "浏览游戏库" })).toHaveAttribute("href", "/library");
    expect(panel.getByRole("heading", { name: "还没有游玩记录" })).toBeInTheDocument();
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

  it("continues the exact session save with a preview and distinct launch explanation", async () => {
    const panel = await showHome({ ...featured(), description: "", hasSaveStates: true, lastSessionSave: { saveStateId: "save-1", createdAtMs: 1000, activeDurationMs: 60000, screenshotUrl: null, discIndex: 1, discLabel: "光盘 2" } });
    expect(panel.getByRole("button", { name: "继续游玩" })).toHaveAttribute("data-save", "save-1");
    expect(panel.getByText("本次将从存档位置启动")).toBeInTheDocument();
    expect(panel.getByText("将从光盘 2继续")).toBeInTheDocument();
    expect(screen.queryByText("暂无可恢复存档")).not.toBeInTheDocument();
  });
});
