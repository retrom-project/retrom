import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ImmersiveGame, ImmersiveGameList } from "./api";
import { GameListView } from "./game-list-view";

const mocks = vi.hoisted(() => ({ fetchGames: vi.fn(), favorite: vi.fn(), launch: vi.fn(), push: vi.fn(), replace: vi.fn(), replacePlayerDocument: vi.fn() }));

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: mocks.push, replace: mocks.replace }) }));
vi.mock("@/lib/player-document-navigation", () => ({ replaceWithPlayerDocument: mocks.replacePlayerDocument }));
vi.mock("./gamepad-source", () => ({ browserGamepadSource: { subscribe: () => () => undefined } }));
vi.mock("./api", () => ({
  ImmersiveAPIError: class extends Error {constructor(public status: number, message: string) {super(message);}},
  fetchImmersiveGames: mocks.fetchGames,
  launchImmersiveGame: mocks.launch,
  setImmersiveFavorite: mocks.favorite,
}));

function game(index: number): ImmersiveGame {
  return {
    gameId: `00000000-0000-7000-8000-${String(index).padStart(12, "0")}`,
    title: `游戏 ${String(index).padStart(2, "0")}`,
    titleInitial: "Y",
    description: index === 0 ? "Retrom 测试简介" : "",
    releaseYear: null,
    developer: "",
    genre: "",
    platformInstance: { id: "gba-directory", name: "GBA 游戏" },
    defaultCore: { id: "mgba", name: "mGBA" },
    coverUrl: null,
    videoUrl: null,
    lastPlayedAtMs: null,
    favorited: false,
    saveStates: [],
  };
}

function page(items: ImmersiveGame[], nextCursor: string | null): ImmersiveGameList {
  return {
    generatedAtMs: 1_000,
    platform: {
      platformId: "gba",
      platformName: "Game Boy Advance",
      gameCount: 60,
      lastPlayedAtMs: null,
      featuredGames: [],
    },
    items,
    nextCursor,
  };
}

function matchMedia() {
  vi.stubGlobal("matchMedia", vi.fn(() => ({
    matches: true,
    media: "",
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })));
}

beforeEach(() => {
  matchMedia();
  mocks.launch.mockResolvedValue("/play/0198abcd-1234-7123-8abc-1234567890ab?experience=immersive");
  vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
  vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
});

afterEach(() => {
  cleanup();
  mocks.fetchGames.mockReset();
  mocks.favorite.mockReset();
  mocks.push.mockReset();
  mocks.replace.mockReset();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("immersive game list view", () => {
  it("restores the requested game and keeps Up/Down from wrapping", async () => {
    mocks.fetchGames.mockResolvedValue(page([game(0), game(1), game(2)], null));
    render(<GameListView platformId="gba" initialGameId={game(1).gameId} />);
    await screen.findByRole("listbox", { name: "Game Boy Advance 游戏" });
    expect(screen.getByRole("option", { selected: true })).toHaveTextContent("游戏 01");
    await waitFor(() => expect(screen.getByRole("option", { selected: true })).toHaveFocus());
    fireEvent.keyDown(window, { key: "ArrowDown" });
    fireEvent.keyDown(window, { key: "ArrowDown" });
    expect(screen.getByRole("option", { selected: true })).toHaveTextContent("游戏 02");
    fireEvent.keyDown(window, { key: "ArrowUp" });
    expect(screen.getByRole("option", { selected: true })).toHaveTextContent("游戏 01");
  });

  it("uses left and right to move by one visible page", async () => {
    mocks.fetchGames.mockResolvedValue(page(Array.from({ length: 20 }, (_, index) => game(index)), null));
    render(<GameListView platformId="gba" />);
    await screen.findByRole("listbox", { name: "Game Boy Advance 游戏" });
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByRole("option", { selected: true })).toHaveTextContent("游戏 08");
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByRole("option", { selected: true })).toHaveTextContent("游戏 16");
    fireEvent.keyDown(window, { key: "ArrowLeft" });
    expect(screen.getByRole("option", { selected: true })).toHaveTextContent("游戏 08");
  });

  it("applies the first browse direction received while a returned list is loading", async () => {
    let resolveInitial: (value: ImmersiveGameList) => void = () => undefined;
    mocks.fetchGames.mockReturnValue(new Promise<ImmersiveGameList>((resolve) => {resolveInitial = resolve;}));
    render(<GameListView platformId="gba" initialGameId={game(0).gameId} />);
    fireEvent.keyDown(window, { key: "ArrowDown" });
    resolveInitial(page([game(0), game(1)], null));
    await waitFor(() => expect(screen.getByRole("option", { selected: true })).toHaveTextContent("游戏 01"));
  });

  it("starts only one cursor request while prefetch is in flight", async () => {
    let resolveNext: (value: ImmersiveGameList) => void = () => undefined;
    const pending = new Promise<ImmersiveGameList>((resolve) => {resolveNext = resolve;});
    mocks.fetchGames.mockResolvedValueOnce(page(Array.from({ length: 41 }, (_, index) => game(index)), "next"));
    mocks.fetchGames.mockReturnValueOnce(pending);
    render(<GameListView platformId="gba" />);
    await screen.findByRole("listbox", { name: "Game Boy Advance 游戏" });
    for (let index = 0; index < 40; index += 1) {fireEvent.keyDown(window, { key: "ArrowDown" });}
    await waitFor(() => expect(mocks.fetchGames).toHaveBeenCalledTimes(2));
    fireEvent.keyDown(window, { key: "ArrowDown" });
    expect(mocks.fetchGames).toHaveBeenCalledTimes(2);
    resolveNext(page(Array.from({ length: 19 }, (_, index) => game(index + 41)), null));
    await waitFor(() => expect(screen.getAllByRole("option")).toHaveLength(60));
  });

  it("toggles the selected platform game's default favorite with Y", async () => {
    const selected = game(0);
    mocks.favorite.mockResolvedValue(undefined);
    mocks.fetchGames.mockResolvedValue(page([selected], null));
    render(<GameListView platformId="gba" />);
    await screen.findByRole("option", { selected: true });
    fireEvent.keyDown(window, { key: "Y" });
    await waitFor(() => expect(mocks.favorite).toHaveBeenCalledWith(selected.gameId, true));
    expect(screen.getByLabelText("已收藏")).toBeInTheDocument();
  });

  it("keeps fullscreen while soft-routing to a Launch", async () => {
    const selected = game(0);
    mocks.fetchGames.mockResolvedValue(page([selected], null));
    render(<GameListView platformId="gba" />);
    await screen.findByRole("option", { selected: true });
    fireEvent.keyDown(window, { key: "Enter" });

    await waitFor(() => expect(mocks.replacePlayerDocument).toHaveBeenCalledWith(
      "/play/0198abcd-1234-7123-8abc-1234567890ab?experience=immersive",
      mocks.replace,
    ));
  });
});
