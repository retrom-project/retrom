export type FavoriteScope = "ALL" | "UNCATEGORIZED" | "FOLDER";
export type FavoriteSort = "FAVORITED_DESC" | "RECENTLY_PLAYED_DESC" | "TITLE_ASC" | "RELEASE_YEAR_DESC";

export type FavoriteQuery = {
  scope: FavoriteScope;
  folderId: string;
  q: string;
  platformId: string;
  sort: FavoriteSort;
};

const scopes = new Set<FavoriteScope>(["ALL", "UNCATEGORIZED", "FOLDER"]);
const sorts = new Set<FavoriteSort>(["FAVORITED_DESC", "RECENTLY_PLAYED_DESC", "TITLE_ASC", "RELEASE_YEAR_DESC"]);
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

export function favoriteQuery(values: Record<string, string | string[] | undefined>): FavoriteQuery {
  const rawScope = typeof values.scope === "string" ? values.scope : "";
  const scope = scopes.has(rawScope as FavoriteScope) ? rawScope as FavoriteScope : "ALL";
  const rawSort = typeof values.sort === "string" ? values.sort : "";
  const sort = sorts.has(rawSort as FavoriteSort) ? rawSort as FavoriteSort : "FAVORITED_DESC";
  const rawFolder = typeof values.folderId === "string" ? values.folderId : "";
  const folderId = scope === "FOLDER" ? rawFolder : "";
  const rawQuery = typeof values.q === "string" ? values.q : "";
  return {
    scope,
    folderId,
    q: Array.from(rawQuery).slice(0, 200).join(""),
    platformId: typeof values.platformId === "string" ? values.platformId : "",
    sort,
  };
}

export function favoriteQueryString(query: FavoriteQuery, cursor = "") {
  const values = new URLSearchParams();
  if (query.scope !== "ALL") values.set("scope", query.scope);
  if (query.scope === "FOLDER" && uuid.test(query.folderId)) values.set("folderId", query.folderId);
  if (query.q.trim()) values.set("q", query.q.trim());
  if (query.platformId) values.set("platformId", query.platformId);
  if (query.sort !== "FAVORITED_DESC") values.set("sort", query.sort);
  if (cursor) values.set("cursor", cursor);
  values.set("limit", "50");
  return values.toString();
}

export function selectFavoriteScope(query: FavoriteQuery, scope: FavoriteScope, folderId = ""): FavoriteQuery {
  return { ...query, scope, folderId: scope === "FOLDER" ? folderId : "" };
}

export function toggleGameSelection(selected: ReadonlySet<string>, gameId: string) {
  const next = new Set(selected);
  if (next.has(gameId)) next.delete(gameId);
  else next.add(gameId);
  return next;
}
