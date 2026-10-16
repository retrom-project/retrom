import type { ComponentProps, ReactNode } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { Game, Save } from "@/lib/api/types";
import type { HomeData } from "./home-data";
import { HomeFeatured, featuredGameId } from "./home-sections";

vi.mock("@/features/player/launch-button", () => ({
  LaunchButton: ({ children, gameId, saveId, coreId, restoreWarning, disabled }: {
    children: ReactNode; gameId: string; saveId?: string; coreId?: string; restoreWarning?: string; disabled?: boolean;
  }) => <button disabled={disabled} data-game={gameId} data-save={saveId} data-core={coreId} data-warning={restoreWarning}>{children}</button>,
}));
afterEach(cleanup);

const game: Game = {
  id: "latest", platformInstanceId: "directory", platformId: "nes", directoryName: "NES 游戏",
  title: "最近实际玩过的游戏", description: "", developer: "", publisher: "", genre: "", players: null,
  releaseYear: null, status: "published", source: "server_import", version: 1,
  contentHash: "a".repeat(64), tags: [], media: [], favorite: false, lastPlayedAtMs: 1000,
  createdAtMs: 0, updatedAtMs: 0,
};
const save: Save = {
  id: "save", name: "进度", kind: "checkpoint", slot: null, version: 1, sizeBytes: 100,
  createdAtMs: 0, updatedAtMs: 2000, screenshotUrl: "/save.png", restorable: true,
  restoreReason: "", restoreWarning: "", game,
  extinfo: { coreId: "fceumm", providerId: "emulatorjs", targetId: "fceumm",
    coreFingerprint: "b".repeat(64), romHash: game.contentHash, checkpointFormat: "state-storage-v1",
    content: { kind: "SINGLE_FILE", entryFile: "game.nes" }, runtimeOptions: {} },
};
function home(selectedSave?: Save, selectedGame = game) {
  const recent = { game: selectedGame, lastPlayedAtMs: 1000 };
  return {
    recent: [recent], saves: [{ ...save, game: { ...game, id: "older", title: "另一款有存档的游戏" } }],
    favorites: [], directories: [], summary: { gameCount: 2, saveCount: 1 },
    featured: { recent, save: selectedSave },
  } satisfies HomeData;
}

it("features the most recent play even when another game's save is newer", () => {
  const data = home();
  expect(featuredGameId(data)).toBe(game.id);
  render(<HomeFeatured data={data} />);
  expect(screen.getByRole("heading", { name: game.title })).toBeInTheDocument();
  const launch = screen.getByRole("button", { name: "新的开始" });
  expect(launch).toHaveAttribute("data-game", game.id);
  expect(launch).not.toHaveAttribute("data-save");
  expect(launch).not.toHaveAttribute("data-core");
  expect(screen.getByText("最近玩过")).toBeInTheDocument();
  expect(screen.getByText(/上次游玩/u)).toHaveTextContent("1970");
  expect(screen.getByText("本次将启动全新游戏")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "查看存档" })).toHaveAttribute("href", `/saves?gameId=${game.id}`);
});

it.each(["checkpoint", "game_save"] as const)("launches the selected %s save with its timestamp in the original metadata row", (kind) => {
  render(<HomeFeatured data={home({ ...save, kind, restoreWarning: "core_changed" })} />);
  const launch = screen.getByRole("button", { name: "从存档继续" });
  expect(launch).toHaveAttribute("data-save", save.id);
  expect(launch).toHaveAttribute("data-core", "fceumm");
  expect(launch).toHaveAttribute("data-warning", "core_changed");
  const times = Array.from(document.querySelectorAll("time"));
  expect(times.map(time => time.dateTime)).toEqual([new Date(2000).toISOString()]);
  expect(screen.getByRole("link", { name: "查看存档" })).toHaveAttribute("href", `/saves?gameId=${game.id}`);
  expect(screen.getByText(/核心版本已变化/u)).toBeInTheDocument();
});

it("explains that native progress is read inside the game", () => {
  render(<HomeFeatured data={home({ ...save, kind: "game_save" })} />);
  expect(screen.getByText("启动后请在游戏内读取进度。")).toBeInTheDocument();
});

it("keeps the newest unavailable save visible without attempting to restore it", () => {
  render(<HomeFeatured data={home({ ...save, restorable: false, restoreReason: "content_changed" })} />);
  expect(screen.getByRole("button", { name: "从存档继续" })).toBeDisabled();
  expect(screen.getByText(/最新存档暂不可恢复/u)).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "查看存档" })).toBeInTheDocument();
  expect(screen.getByRole("img", { name: `${game.title}的存档画面` })).toHaveAttribute("src", new URL(save.screenshotUrl!, location.origin).href);
});

it("uses platform artwork whenever there are no saves, even if the game has a cover or screenshots", () => {
  const withMedia = { ...game, media: [
    { id: "cover", kind: "cover", url: "/cover.png", mediaType: "image/png", ordinal: 0 },
    { id: "scene", kind: "screenshot", url: "/scene.png", mediaType: "image/png", ordinal: 0 },
  ] } satisfies Game;
  const view = render(<HomeFeatured data={home(undefined, withMedia)} />);
  expect(view.container.querySelector(".home-featured-media")).toHaveAttribute("data-kind", "platform");
  expect(view.container.querySelector(".home-featured-art")).toBeNull();
});

it("keeps the selected save when its screenshot fails, without substituting platform artwork", () => {
  const view = render(<HomeFeatured data={home(save)} />);
  fireEvent.error(screen.getByRole("img", { name: `${game.title}的存档画面` }));
  expect(view.container.querySelector(".home-featured-media")).toHaveAttribute("data-kind", "save");
  expect(screen.getByText("存档截图暂不可用")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "从存档继续" })).toHaveAttribute("data-save", save.id);
  view.rerender(<HomeFeatured data={home({ ...save, version: 2 })} />);
  expect(screen.getByRole("img", { name: `${game.title}的存档画面` })).toHaveAttribute("src", new URL(save.screenshotUrl!, location.origin).href);
});

it("does not substitute platform artwork when a save has no screenshot", () => {
  const view = render(<HomeFeatured data={home({ ...save, screenshotUrl: null })} />);
  expect(view.container.querySelector(".home-featured-media")).toHaveAttribute("data-kind", "save");
  expect(screen.getByText("存档截图暂不可用")).toBeInTheDocument();
});

it("shows the browse-library empty state when there is no actual play history", () => {
  const data = { ...home(), recent: [], featured: null } as ComponentProps<typeof HomeFeatured>["data"];
  render(<HomeFeatured data={data} />);
  expect(screen.getByRole("link", { name: "浏览游戏库" })).toBeInTheDocument();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});
