import type { SaveFilters } from "./save-library";

export function saveURLFilters(params: URLSearchParams): SaveFilters {
  const availability = params.get("availability");
  return {query: params.get("q") ?? "", gameId: params.get("gameId") ?? "",
    availability: availability === "ALL" || availability === "BLOCKED" ? availability : "AVAILABLE",
    sort: params.get("sort") === "CREATED_ASC" ? "CREATED_ASC" : "CREATED_DESC"};
}

export function saveURLQuery(filters: SaveFilters) {
  const params = new URLSearchParams();
  if (filters.query.trim()) {params.set("q", filters.query.trim());}
  if (filters.gameId) {params.set("gameId", filters.gameId);}
  if (filters.availability !== "AVAILABLE") {params.set("availability", filters.availability);}
  if (filters.sort !== "CREATED_DESC") {params.set("sort", filters.sort);}
  return params.toString();
}
