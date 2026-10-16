import type { ComponentProps, ReactNode } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { Game, Save, Schema } from "@/lib/api/types";
import { HomeFeatured, featuredGameId } from "./home-sections";

vi.mock("@/features/player/launch-button", () => ({
  LaunchButton: ({ children, gameId, saveId, coreId, restoreWarning }: {
    children: ReactNode; gameId: string; saveId?: string; coreId?: string; restoreWarning?: string;
  }) => <button data-game={gameId} data-save={saveId} data-core={coreId} data-warning={restoreWarning}>{children}</button>,
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
function home(selectedSave?: Save, selectedGame = game, saveCount = selectedSave ? 1 : 0) {
  const recent = { game: selectedGame, lastPlayedAtMs: 1000 };
  return {
    recent: [recent], saves: [{ ...save, game: { ...game, id: "older", title: "另一款有存档的游戏" } }],
    favorites: [], directories: [], summary: { gameCount: 2, saveCount: 1 },
    featured: { recent, save: selectedSave, saveCount },
  } satisfies Schema<"Home"> & { featured: { recent: Schema<"RecentGame">; save?: Save; saveCount: number } };
}

it("features the most recent play even when another game's save is newer", () => {
  const data = home();
  expect(featuredGameId(data)).toBe(game.id);
  render(<HomeFeatured data={data} />);
  expect(screen.getByRole("heading", { name: game.title })).toBeInTheDocument();
  const launch = screen.getByRole("button", { name: "开始游戏" });
  expect(launch).toHaveAttribute("data-game", game.id);
  expect(launch).not.toHaveAttribute("data-save");
  expect(launch).not.toHaveAttribute("data-core");
  expect(screen.getByText("最近游玩")).toBeInTheDocument();
  expect(screen.getByText(/上次游玩/u)).toHaveTextContent("1970");
  expect(screen.getByText("暂无存档")).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "查看存档" })).not.toBeInTheDocument();
});

it.each(["checkpoint", "game_save"] as const)("launches the selected %s save and distinguishes play time from save time", (kind) => {
  render(<HomeFeatured data={home({ ...save, kind, restoreWarning: "core_changed" })} />);
  const launch = screen.getByRole("button", { name: "从存档继续" });
  expect(launch).toHaveAttribute("data-save", save.id);
  expect(launch).toHaveAttribute("data-core", "fceumm");
  expect(launch).toHaveAttribute("data-warning", "core_changed");
  const times = Array.from(document.querySelectorAll("time"));
  expect(times.map(time => time.dateTime)).toEqual([new Date(1000).toISOString(), new Date(2000).toISOString()]);
  expect(screen.getByRole("link", { name: "查看存档" })).toHaveAttribute("href", `/saves?gameId=${game.id}`);
  expect(screen.getByText(/核心版本已变化/u)).toBeInTheDocument();
});

it("explains that native progress is read inside the game", () => {
  render(<HomeFeatured data={home({ ...save, kind: "game_save" })} />);
  expect(screen.getByText("启动后请在游戏内读取进度。")).toBeInTheDocument();
});

it("keeps existing unavailable saves reachable without trying to restore them", () => {
  render(<HomeFeatured data={home(undefined, game, 2)} />);
  expect(screen.getByRole("button", { name: "开始游戏" })).not.toHaveAttribute("data-save");
  expect(screen.getByText("存档暂不可恢复")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "查看存档" })).toBeInTheDocument();
});

it("falls back from a failed save image to screenshots, full cover, then platform artwork", () => {
  const mediaGame = { ...game, media: [
    { id: "cover", kind: "cover", url: "/cover.png", mediaType: "image/png", ordinal: 0 },
    { id: "scene", kind: "screenshot", url: "/scene.png", mediaType: "image/png", ordinal: 0 },
  ] } satisfies Game;
  const view = render(<HomeFeatured data={home(save, mediaGame)} />);
  for (const [kind, url] of [["save", "/save.png"], ["screenshot", "/scene.png"], ["cover", "/cover.png"]]) {
    const media = view.container.querySelector(".home-featured-media")!;
    expect(media).toHaveAttribute("data-kind", kind);
    expect(Boolean(screen.queryByText("存档画面"))).toBe(kind === "save");
    const image = media.querySelector(".home-featured-art")!;
    expect(new URL(image.getAttribute("src")!, location.origin).pathname).toBe(url);
    fireEvent.error(image);
  }
  expect(view.container.querySelector(".home-featured-media")).toHaveAttribute("data-kind", "platform");
  expect(screen.getByRole("button", { name: "从存档继续" })).toHaveAttribute("data-save", save.id);
});

it("resets failed artwork when the featured game changes", () => {
  const withCover = { ...game, media: [{ id: "cover", kind: "cover", url: "/cover.png", mediaType: "image/png", ordinal: 0 }] } satisfies Game;
  const view = render(<HomeFeatured data={home(undefined, withCover)} />);
  fireEvent.error(view.container.querySelector(".home-featured-art")!);
  view.rerender(<HomeFeatured data={home(undefined, { ...withCover, id: "another" })} />);
  expect(view.container.querySelector(".home-featured-media")).toHaveAttribute("data-kind", "cover");
});

it("shows the browse-library empty state when there is no actual play history", () => {
  const data = { ...home(), recent: [], featured: null } as ComponentProps<typeof HomeFeatured>["data"];
  render(<HomeFeatured data={data} />);
  expect(screen.getByRole("link", { name: "浏览游戏库" })).toBeInTheDocument();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});
