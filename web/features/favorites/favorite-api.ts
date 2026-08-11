import type { components } from "@/lib/api/generated/schema";
import { newUuid } from "@/lib/crypto";

export type FavoriteReference = components["schemas"]["FavoriteReference"];
export type FavoriteState = components["schemas"]["FavoriteState"];
export type FavoriteFolder = components["schemas"]["FavoriteFolder"];
export type FavoritePage = components["schemas"]["FavoriteListResponse"];
export type FavoriteGame = components["schemas"]["FavoriteGameItem"];
export type UnfavoriteResult = components["schemas"]["UnfavoriteResult"];
export type RestoreResult = components["schemas"]["FavoriteRestoreResult"];
export type BatchResult = components["schemas"]["FavoriteBatchResult"];

export type AuthenticatedFetch = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

export class FavoriteAPIError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
    public readonly requestId?: string,
  ) {
    super(message);
    this.name = "FavoriteAPIError";
  }
}

async function parseError(response: Response) {
  const payload = await response.json().catch(() => null) as {
    error?: { code?: string; message?: string; requestId?: string };
  } | null;
  throw new FavoriteAPIError(
    response.status,
    payload?.error?.code ?? "UNKNOWN_ERROR",
    payload?.error?.message ?? `收藏请求失败（HTTP ${response.status}）`,
    payload?.error?.requestId,
  );
}

async function request<T>(
  authenticatedFetch: AuthenticatedFetch,
  path: string,
  options: { method?: string; body?: unknown; idempotent?: boolean; etag?: string; signal?: AbortSignal } = {},
) {
  const headers = new Headers({ Accept: "application/json" });
  const method = options.method ?? "GET";
  if (options.body !== undefined) headers.set("Content-Type", "application/json");
  if (options.idempotent) headers.set("Idempotency-Key", newUuid());
  if (options.etag) headers.set("If-Match", options.etag);
  const response = await authenticatedFetch(path, {
    method,
    cache: "no-store",
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    signal: options.signal,
  });
  if (!response.ok) await parseError(response);
  if (response.status === 204) return { data: null as T, response };
  return { data: await response.json() as T, response };
}

export function loadFavorites(authenticatedFetch: AuthenticatedFetch, query: string, signal?: AbortSignal) {
  return request<FavoritePage>(authenticatedFetch, `/api/v1/favorites${query ? `?${query}` : ""}`, { signal });
}

export function putFavorite(authenticatedFetch: AuthenticatedFetch, gameId: string) {
  return request<FavoriteState>(authenticatedFetch, `/api/v1/favorites/${gameId}`, { method: "PUT", body: {} });
}

export function replaceFavoriteFolders(authenticatedFetch: AuthenticatedFetch, gameId: string, folderIds: string[]) {
  return request<FavoriteState>(authenticatedFetch, `/api/v1/favorites/${gameId}/folders`, {
    method: "PUT", body: { folderIds },
  });
}

export function organizeFavorites(
  authenticatedFetch: AuthenticatedFetch,
  gameIds: string[],
  addFolderIds: string[],
  removeFolderIds: string[],
) {
  return request<BatchResult>(authenticatedFetch, "/api/v1/favorites/organize", {
    method: "POST", idempotent: true, body: { gameIds, addFolderIds, removeFolderIds },
  });
}

export function unfavoriteGames(authenticatedFetch: AuthenticatedFetch, gameIds: string[]) {
  return request<UnfavoriteResult>(authenticatedFetch, "/api/v1/favorites/unfavorite", {
    method: "POST", idempotent: true, body: { gameIds },
  });
}

export function restoreFavorites(
  authenticatedFetch: AuthenticatedFetch,
  items: UnfavoriteResult["items"],
) {
  return request<RestoreResult>(authenticatedFetch, "/api/v1/favorites/restore", {
    method: "POST", idempotent: true, body: { items },
  });
}

export function createFavoriteFolder(
  authenticatedFetch: AuthenticatedFetch,
  name: string,
  initialGameIds: string[],
) {
  return request<FavoriteFolder>(authenticatedFetch, "/api/v1/favorite-folders", {
    method: "POST", idempotent: true, body: { name, initialGameIds },
  });
}

export function renameFavoriteFolder(
  authenticatedFetch: AuthenticatedFetch,
  folderId: string,
  version: number,
  name: string,
) {
  return request<FavoriteFolder>(authenticatedFetch, `/api/v1/favorite-folders/${folderId}`, {
    method: "PATCH", idempotent: true, etag: `"v${version}"`, body: { name },
  });
}

export function deleteFavoriteFolder(
  authenticatedFetch: AuthenticatedFetch,
  folderId: string,
  version: number,
) {
  return request<null>(authenticatedFetch, `/api/v1/favorite-folders/${folderId}`, {
    method: "DELETE", idempotent: true, etag: `"v${version}"`, body: {},
  });
}
