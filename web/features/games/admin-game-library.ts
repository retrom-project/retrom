import { formatLibraryPlayedAt, type GameSummary } from "@/features/library/game-library";

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
};

export async function collectAdminGamePages(loadPage: (cursor: string | null) => Promise<AdminGamePage>) {
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

function stableTitleOrder(left: AdminGameSummary, right: AdminGameSummary) {
  return left.title.localeCompare(right.title, "zh-CN") || left.gameId.localeCompare(right.gameId);
}

export function filterAdminGames(games: AdminGameSummary[], filters: AdminGameFilters) {
  const query = filters.query.trim().toLocaleLowerCase("zh-CN");
  return games
    .filter((game) => matchesAdminGame(game, filters, query))
    .sort((left, right) => compareAdminGames(left, right, filters.sort));
}

function matchesAdminGame(game: AdminGameSummary, filters: AdminGameFilters, query: string) {
  if (filters.platformId && game.platform.id !== filters.platformId) {return false;}
  if (filters.platformInstanceId && game.platformInstance.id !== filters.platformInstanceId) {return false;}
  if (filters.tagId && !(game.tags ?? []).some((tag) => tag.tagId === filters.tagId)) {return false;}
  if (filters.visibility !== "ALL" && game.status !== filters.visibility) {return false;}
  if (!matchesRuntimeFilter(game, filters.runtime)) {return false;}
  if (!query) {return true;}
  return matchesAdminGameSearch(game, query);
}

function matchesRuntimeFilter(game: AdminGameSummary, runtime: AdminGameFilters["runtime"]) {
  if (runtime === "ALL") {return true;}
  if (runtime === "DELETED") {return game.status === "DELETED";}
  if (game.status === "DELETED") {return false;}
  return runtime === "READY" ? game.runtimeStatus === "READY" : game.runtimeStatus !== "READY";
}

function matchesAdminGameSearch(game: AdminGameSummary, query: string) {
  const searchable = [
    game.title, game.platform.name, game.platformInstance.name, game.defaultCore.name,
    ...(game.tags ?? []).map((tag) => tag.name),
  ];
  return searchable.some((value) => value.toLocaleLowerCase("zh-CN").includes(query));
}

function compareAdminGames(left: AdminGameSummary, right: AdminGameSummary, sort: AdminGameFilters["sort"]) {
  if (sort === "TITLE_ASC") {return stableTitleOrder(left, right);}
  if (sort === "ADDED_DESC") {return right.createdAtMs - left.createdAtMs || stableTitleOrder(left, right);}
  return right.updatedAtMs - left.updatedAtMs || stableTitleOrder(left, right);
}

export function adminGameSummary(games: AdminGameSummary[]) {
  return {
    total: games.length,
    runtimeAttention: games.filter((game) => game.status !== "DELETED" && game.runtimeStatus !== "READY").length,
    missingCover: games.filter((game) => !game.coverUrl).length,
    incompleteMetadata: games.filter((game) => !game.metadataComplete).length,
    hidden: games.filter((game) => game.status !== "PUBLISHED").length,
  };
}

export function adminGamePlatforms(games: AdminGameSummary[]) {
  const values = new Map<string, { id: string; name: string }>();
  for (const game of games) {values.set(game.platform.id, game.platform);}
  return [...values.values()].sort((left, right) => left.name.localeCompare(right.name, "zh-CN") || left.id.localeCompare(right.id));
}

export function adminGameDirectories(games: AdminGameSummary[], platformId: string) {
  const values = new Map<string, { id: string; name: string; platformId: string }>();
  for (const game of games) {
    if (platformId && game.platform.id !== platformId) {continue;}
    values.set(game.platformInstance.id, { ...game.platformInstance, platformId: game.platform.id });
  }
  return [...values.values()].sort((left, right) => left.name.localeCompare(right.name, "zh-CN") || left.id.localeCompare(right.id));
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
