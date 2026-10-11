import { api, result } from "@/lib/api/client";
import { loadDirectories, loadGames } from "@/features/library/api";
import type { Schema } from "@/lib/api/types";
export type View = "platforms" | "games" | "favorites" | "saves" | "recent";
export type ImmersiveEntry = { game: Schema<"Game">; save?: Schema<"Save">; lastPlayedAtMs?: number };
export type ImmersivePage = { items: ImmersiveEntry[]; offset: number; limit: number; total: number };
export function entryId(entry: ImmersiveEntry | undefined) { return entry?.game.id ?? ""; }
export type Destination = { id: string; name: string; code: string; view: Exclude<View, "platforms">; directoryId?: string; count: number; featured: Schema<"Game">[] };
export async function loadFolders() {
  return result(await api.GET("/api/v1/favorite-folders"));
}
export async function loadEntries(view: View, directory: string | undefined, folder: string, offset: number, limit = 24, signal?: AbortSignal): Promise<ImmersivePage> {
  if (view === "platforms") { return { items: [], offset, limit, total: 0 }; }
  if (view === "saves") {
    const page = result(await api.GET("/api/v1/saves", { params: { query: { offset, limit, groupByGame: true } }, signal }));
    return { ...page, items: page.items.map((save) => ({ game: save.game, save })) };
  }
  if (view === "recent") {
    const page = result(await api.GET("/api/v1/recent-games", { params: { query: { offset, limit, sort: "recent" } }, signal }));
    return { ...page, items: page.items.map((item) => ({ game: item.game, lastPlayedAtMs: item.lastPlayedAtMs })) };
  }
  const page = await loadGames(view === "favorites" ? "favorites" : "library", { q: "", offset, limit, platformInstanceId: view === "games" ? directory : undefined, folderId: folder && folder !== "unclassified" ? folder : undefined, unclassified: folder === "unclassified" || undefined }, signal);
  return { ...page, items: page.items.map((game) => ({ game })) };
}
const libraryDestinations = [
  { id: "all", name: "全部游戏", code: "ALL", view: "games" },
  { id: "recent", name: "最近游玩", code: "RECENT", view: "recent" },
  { id: "favorites", name: "收藏游戏", code: "FAVORITES", view: "favorites" },
  { id: "saves", name: "我的存档", code: "SAVES", view: "saves" },
] as const;
export async function loadDestinations(): Promise<{ items: Destination[] }> {
  const [directories, libraries] = await Promise.all([
    loadDirectories(),
    Promise.all(libraryDestinations.map(async (item) => {
      const page = await loadEntries(item.view, undefined, "", 0, 3);
      return { ...item, count: page.total, featured: page.items.map((entry) => entry.game) };
    })),
  ]);
  if (libraries[0].count === 0) { return { items: [] }; }
  return { items: [...libraries, ...directories.items.map((item): Destination => ({ id: item.id, name: item.name, code: item.platformId.toUpperCase(), view: "games", directoryId: item.id, count: item.gameCount, featured: [] }))] };
}
