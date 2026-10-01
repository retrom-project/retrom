import { withQuery } from "@/lib/backend";
import { formatLibraryPlayedAt, type GameSummary, type LibraryFacets } from "@/features/library/game-library";

export type AdminGameSummary = GameSummary & {
  version: number;
  updatedAtMs: number;
  releaseYear: number | null;
  metadataComplete: boolean;
  runtimeStatus: string | null;
};

export type AdminGameFilters = {
  query: string;
  platformId: string;
  platformInstanceId: string;
  tagId?: string;
  visibility: "ALL" | "PUBLISHED" | "DELETED";
  runtime: "ALL" | "READY" | "ATTENTION" | "DELETED";
  sort: "UPDATED_DESC" | "TITLE_ASC" | "ADDED_DESC";
};

export type AdminGamePage = {
  generatedAtMs: number;
  items: AdminGameSummary[];
  nextCursor: string | null;
  filteredCount?: number;
  facets?: LibraryFacets;
  summary?: { total: number; runtimeAttention: number; missingCover: number; incompleteMetadata: number; hidden: number };
};

export async function collectAdminGameExport(loadPage: (cursor: string | null) => Promise<AdminGamePage>) {
  const items: AdminGameSummary[] = [];
  const seenCursors = new Set<string>();
  let cursor: string | null = null;
  let generatedAtMs: number | null = null;
  do {
    const page = await loadPage(cursor);
    generatedAtMs ??= page.generatedAtMs;
    items.push(...page.items);
    cursor = page.nextCursor;
    if (cursor && seenCursors.has(cursor)) {throw new Error("Retrom API returned a repeated admin game cursor");}
    if (cursor) {seenCursors.add(cursor);}
  } while (cursor);
  return { generatedAtMs: generatedAtMs ?? 0, items };
}

export function adminGameURL(filters: AdminGameFilters, limit = 6, cursor?: string | null) {
  return withQuery("/api/v1/admin/games", {
    limit: String(limit), q: filters.query.trim(), platformId: filters.platformId,
    platformInstanceId: filters.platformInstanceId, tagId: filters.tagId ?? "",
    status: filters.visibility, runtime: filters.runtime, sort: filters.sort,
    ...(cursor ? { cursor } : {}),
  });
}

export function runtimePresentation(status: string | null, gameStatus = "PUBLISHED") {
  if (gameStatus === "DELETED") {return { label: "已删除", tone: "bad" as const, note: "游戏已删除" };}
  if (status === "READY") {return { label: "可以运行", tone: "good" as const, note: "运行验证已通过" };}
  if (status === "QUEUED" || status === "RUNNING" || status === "PENDING" || status === null) {
    return { label: "待验证", tone: "warn" as const, note: "等待兼容性验证" };
  }
  return { label: "需要处理", tone: "bad" as const, note: "运行环境存在异常" };
}

export function adminGameUpdateNote(game: AdminGameSummary) {
  if (game.status === "DELETED") {return runtimePresentation(game.runtimeStatus, game.status).note;}
  if (!game.metadataComplete) {return "资料不完整";}
  return runtimePresentation(game.runtimeStatus, game.status).note;
}

export function formatAdminGameTime(value: number, nowMs: number, timeZone?: string) {
  return formatLibraryPlayedAt(value, nowMs, timeZone);
}
