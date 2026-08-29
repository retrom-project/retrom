import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ImmersiveGame, ImmersiveLibraryGameList } from "./api";
import { LibraryGameListView } from "./library-game-list-view";

const mocks = vi.hoisted(() => ({
  favorite: vi.fn(),
  fetchGames: vi.fn(),
  launch: vi.fn(),
  push: vi.fn(),
  replace: vi.fn(),
  replacePlayerDocument: vi.fn(),
}));

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: mocks.push, replace: mocks.replace }) }));
vi.mock("@/lib/player-document-navigation", () => ({ replaceWithPlayerDocument: mocks.replacePlayerDocument }));
vi.mock("./gamepad-source", () => ({ browserGamepadSource: { subscribe: () => () => undefined } }));
vi.mock("./api", () => ({
  ImmersiveAPIError: class extends Error {constructor(public status: number, message: string) {super(message);}},
  fetchImmersiveLibraryGames: mocks.fetchGames,
  launchImmersiveGame: mocks.launch,
  setImmersiveFavorite: mocks.favorite,
}));

function game(index: number, saves = 0): ImmersiveGame {
  return {
    gameId: `00000000-0000-7000-8000-${String(index).padStart(12, "0")}`,
    title: `游戏 ${index}`,
    titleInitial: "Y",
    description: "测试简介",
    releaseYear: 2026,
    developer: "Retrom",
    genre: "Action",
    platformInstance: { id: "gba", name: "GBA 游戏" },
    defaultCore: { id: "mgba", name: "mGBA" },
    coverUrl: null,
    videoUrl: null,
    lastPlayedAtMs: 1_000,
    favorited: false,
    saveStates: Array.from({ length: saves }, (_, saveIndex) => ({
      saveStateId: `10000000-0000-7000-8000-${String(saveIndex).padStart(12, "0")}`,
      name: `存档 ${saveIndex + 1}`,
      createdAtMs: 2_000 + saveIndex,
      discIndex: null,
      screenshotUrl: `/content/save-states/save-${saveIndex}/screenshot`,
    })),
  };
}

function page(kind: "all" | "recent" | "favorites" | "saves", items: ImmersiveGame[]): ImmersiveLibraryGameList {
  return {
    generatedAtMs: 2_000,
    library: {
      destinationId: kind,
      kind,
      name: kind === "favorites" ? "收藏游戏" : kind === "saves" ? "我的存档" : "全部游戏",
      gameCount: items.length,
      lastPlayedAtMs: 1_000,
      featuredGames: [],
    },
    folder: null,
    folders: kind === "favorites" ? [{ folderId: "20000000-0000-7000-8000-000000000001", name: "待通关", gameCount: 1 }] : [],
    items,
    nextCursor: null,
  };
}

beforeEach(() => {
  vi.stubGlobal("matchMedia", vi.fn(() => ({
    matches: true,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  })));
  vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
  vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
  mocks.favorite.mockResolvedValue(undefined);
  mocks.launch.mockResolvedValue("/play/launch?experience=immersive");
});

afterEach(() => {
  cleanup();
  for (const mock of Object.values(mocks)) {mock.mockReset();}
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("immersive library game list", () => {
  it("places custom favorite folders before games and opens them with A", async () => {
    mocks.fetchGames.mockResolvedValue(page("favorites", [game(1)]));
    render(<LibraryGameListView kind="favorites" />);
    expect(await screen.findByRole("option", { selected: true })).toHaveTextContent("待通关");
    fireEvent.keyDown(window, { key: "Enter" });
    expect(mocks.push).toHaveBeenCalledWith(
      "/immersive/library/favorites?folderId=20000000-0000-7000-8000-000000000001",
    );
  });

  it("toggles the selected game's default favorite with Y", async () => {
    mocks.fetchGames.mockResolvedValue(page("all", [game(1)]));
    render(<LibraryGameListView kind="all" />);
    await screen.findByRole("option", { selected: true });
    fireEvent.keyDown(window, { key: "Y" });
    await waitFor(() => expect(mocks.favorite).toHaveBeenCalledWith(game(1).gameId, true));
    expect(await screen.findByText("已收藏游戏。")).toBeInTheDocument();
  });

  it("switches saves with left and right and launches the selected save", async () => {
    const savedGame = {
      ...game(2, 2),
      coverUrl: "/content/assets/cover",
      videoUrl: "/content/assets/video",
    };
    mocks.fetchGames.mockResolvedValue(page("saves", [savedGame]));
    render(<LibraryGameListView kind="saves" />);
    const saveList = await screen.findByRole("list", { name: `${savedGame.title} 的存档` });
    expect(screen.getByRole("img", { name: `${savedGame.title} 封面` })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: `${savedGame.title} 视频预览` })).toBeInTheDocument();
    expect(saveList.querySelectorAll("button")).toHaveLength(2);
    expect(screen.getAllByRole("img", { name: /存档截图/ })).toHaveLength(2);
    expect(screen.getByRole("button", { pressed: true })).toHaveAccessibleName(/第 1 份存档/);
    expect(screen.queryByText("存档 1")).not.toBeInTheDocument();
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(screen.getByRole("button", { pressed: true })).toHaveAccessibleName(/第 2 份存档/);
    fireEvent.keyDown(window, { key: "Enter" });
    await waitFor(() => expect(mocks.launch).toHaveBeenCalledWith(
      savedGame.gameId,
      expect.stringContaining(`saveStateId=${encodeURIComponent(savedGame.saveStates[1].saveStateId)}`),
      savedGame.saveStates[1].saveStateId,
    ));
    expect(mocks.replacePlayerDocument).toHaveBeenCalledWith("/play/launch?experience=immersive", mocks.replace);
  });
});
