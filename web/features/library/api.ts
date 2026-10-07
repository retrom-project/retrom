import { api, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
export type GameQuery = {
  q: string;
  offset: number;
  limit: number;
  platformInstanceId?: string;
  platformId?: string;
  tagId?: string;
  sort?: "title" | "recent";
  folderId?: string;
  unclassified?: boolean;
};
export async function loadGames(
  kind: "library" | "favorites" | "admin" | "review",
  query: GameQuery,
) {
  if (kind === "favorites") {
    return result(await api.GET("/api/v1/favorites", { params: { query } }));
  }
  if (kind === "admin") {
    return result(await api.GET("/api/v1/admin/games", { params: { query } }));
  }
  if (kind === "review") {
    return result(
      await api.GET("/api/v1/admin/reviews", { params: { query } }),
    );
  }
  return result(await api.GET("/api/v1/games", { params: { query } }));
}
export async function loadCatalog() {
  return result(await api.GET("/api/v1/runtime/catalog"));
}
export async function loadDirectories() {
  return result(await api.GET("/api/v1/platform-instances"));
}
export async function loadTags() {
  return result(await api.GET("/api/v1/tags"));
}
export async function toggleFavorite(
  gameId: string,
  favorite: boolean,
  folderIds: string[] = [],
) {
  const response = favorite
    ? await api.PUT("/api/v1/favorites/{gameId}", {
        params: { path: { gameId } },
        body: { folderIds },
      })
    : await api.DELETE("/api/v1/favorites/{gameId}", {
        params: { path: { gameId } },
      });
  if (response.error) {
    throw new Error(response.error.message);
  }
}
export async function loadDetail(
  gameId: string,
  mode: "user" | "admin" | "review",
): Promise<Schema<"GameDetail">> {
  const params = { path: { gameId } };
  if (mode === "admin") {
    return result(await api.GET("/api/v1/admin/games/{gameId}", { params }));
  }
  if (mode === "review") {
    return result(await api.GET("/api/v1/admin/reviews/{gameId}", { params }));
  }
  return result(await api.GET("/api/v1/games/{gameId}", { params }));
}
