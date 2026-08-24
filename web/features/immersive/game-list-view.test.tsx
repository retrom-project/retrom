import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ImmersiveGame, ImmersiveGameList } from "./api";
import { GameListView } from "./game-list-view";

const mocks = vi.hoisted(() => ({ fetchGames: vi.fn(), push: vi.fn(), replace: vi.fn() }));

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: mocks.push, replace: mocks.replace }) }));
vi.mock("./gamepad-source", () => ({ browserGamepadSource: { subscribe: () => () => undefined } }));
vi.mock("./api", () => ({
  ImmersiveAPIError: class extends Error {constructor(public status: number, message: string) {super(message);}},
  fetchImmersiveGames: mocks.fetchGames,
  launchImmersiveGame: vi.fn(),
}));

function game(index: number): ImmersiveGame {
  return {
    gameId: `00000000-0000-7000-8000-${String(index).padStart(12, "0")}`,
    title: `游戏 ${String(index).padStart(2, "0")}`,
    description: index === 0 ? "Retrom 测试简介" : "",
    releaseYear: null,
    developer: "",
    genre: "",
    platformInstance: { id: "gba-directory", name: "GBA 游戏" },
    defaultCore: { id: "mgba", name: "mGBA" },
    coverUrl: null,
    videoUrl: null,
    lastPlayedAtMs: null,
  };
}

function page(items: ImmersiveGame[], nextCursor: string | null): ImmersiveGameList {
  return {
    generatedAtMs: 1_000,
    platform: { platformId: "gba", platformName: "Game Boy Advance", gameCount: 60, lastPlayedAtMs: null },
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
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
});

afterEach(() => {
  cleanup();
  mocks.fetchGames.mockReset();
  mocks.push.mockReset();
  mocks.replace.mockReset();
  vi.unstubAllGlobals();
});

describe("immersive game list view", () => {
  it("restores the requested game and keeps Up/Down from wrapping", async () => {
    mocks.fetchGames.mockResolvedValue(page([game(0), game(1), game(2)], null));
    render(<GameListView platformId="gba" initialGameId={game(1).gameId} />);
    await screen.findByRole("listbox", { name: "Game Boy Advance 游戏" });
    expect(screen.getByRole("option", { selected: true })).toHaveTextContent("游戏 01");
    fireEvent.keyDown(window, { key: "ArrowDown" });
    fireEvent.keyDown(window, { key: "ArrowDown" });
    expect(screen.getByRole("option", { selected: true })).toHaveTextContent("游戏 02");
    fireEvent.keyDown(window, { key: "ArrowUp" });
    expect(screen.getByRole("option", { selected: true })).toHaveTextContent("游戏 01");
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
});
