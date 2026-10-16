import { api, result } from "@/lib/api/client";
import type { Save, Schema } from "@/lib/api/types";

export type HomeData = Schema<"Home"> & {
  featured: { recent: Schema<"RecentGame">; save?: Save; saveCount: number } | null;
};

export async function loadHome(signal?: AbortSignal): Promise<HomeData> {
  const home = result(await api.GET("/api/v1/home", { signal }));
  const recent = home.recent[0];
  if (!recent) { return { ...home, featured: null }; }
  // The home response contains only the latest saves across all games. Query
  // this game's history so another game's newer saves cannot hide its progress.
  let offset = 0;
  while (true) {
    const page = result(await api.GET("/api/v1/saves", {
      signal,
      params: { query: { gameId: recent.game.id, sort: "recent", offset, limit: 100 } },
    }));
    const save = page.items.find(item => item.restorable);
    offset += page.items.length;
    if (save || !page.items.length || offset >= page.total) {
      return { ...home, featured: { recent, save, saveCount: page.total } };
    }
  }
}
