import { api, result } from "@/lib/api/client";
import type { Save, Schema } from "@/lib/api/types";

export type HomeData = Schema<"Home"> & {
  featured: { recent: Schema<"RecentGame">; save?: Save } | null;
};

export async function loadHome(signal?: AbortSignal): Promise<HomeData> {
  const home = result(await api.GET("/api/v1/home", { signal }));
  const recent = home.recent[0];
  if (!recent) { return { ...home, featured: null }; }
  // The home response only contains a global slice of saves. Fetch this game's
  // latest save so its timestamp, screenshot and resume action share one source.
  const saves = result(await api.GET("/api/v1/saves", {
    signal,
    params: { query: { gameId: recent.game.id, sort: "recent", limit: 1 } },
  }));
  return { ...home, featured: { recent, save: saves.items[0] } };
}
