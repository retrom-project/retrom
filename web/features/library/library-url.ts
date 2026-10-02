import type { LibraryFilters } from "./game-library";

export function libraryURLFilters(params: URLSearchParams): LibraryFilters {
  const sort = params.get("sort");
  return {query: params.get("q") ?? "", platformId: params.get("platformId") ?? "",
    platformInstanceId: params.get("platformInstanceId") ?? "", tagId: params.get("tagId") ?? "",
    sort: sort === "ADDED_DESC" || sort === "TITLE_ASC" ? sort : "RECENT_DESC"};
}

export function libraryURLQuery(filters: LibraryFilters) {
  const params = new URLSearchParams();
  if (filters.query.trim()) {params.set("q", filters.query.trim());}
  if (filters.platformId) {params.set("platformId", filters.platformId);}
  if (filters.platformInstanceId) {params.set("platformInstanceId", filters.platformInstanceId);}
  if (filters.tagId) {params.set("tagId", filters.tagId);}
  if (filters.sort !== "RECENT_DESC") {params.set("sort", filters.sort);}
  return params.toString();
}
