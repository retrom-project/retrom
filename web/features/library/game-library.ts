export type GameSummary = {
  gameId: string;
  title: string;
  platform: { id: string; name: string };
  platformInstance: { id: string; name: string };
  defaultCore: { id: string; name: string };
  status: string;
  coverUrl: string | null;
  createdAtMs: number;
  lastPlayedAtMs: number | null;
  favorite: import("@/features/favorites/favorite-api").FavoriteReference | null;
};

export type LibraryFilters = {
  query: string;
  platformId: string;
  platformInstanceId: string;
  sort: "RECENT_DESC" | "ADDED_DESC" | "TITLE_ASC";
};

export type GamePage = {
  generatedAtMs: number;
  items: GameSummary[];
  nextCursor: string | null;
};

export async function collectGamePages(loadPage: (cursor: string | null) => Promise<GamePage>) {
  const items: GameSummary[] = [];
  const seenCursors = new Set<string>();
  let cursor: string | null = null;
  let generatedAtMs: number | null = null;
  do {
    const page = await loadPage(cursor);
    generatedAtMs ??= page.generatedAtMs;
    items.push(...page.items);
    cursor = page.nextCursor;
    if (cursor && seenCursors.has(cursor)) throw new Error("Retrom API returned a repeated game cursor");
    if (cursor) seenCursors.add(cursor);
  } while (cursor);
  return { generatedAtMs: generatedAtMs ?? 0, items };
}

function stableTitleOrder(left: GameSummary, right: GameSummary) {
  return left.title.localeCompare(right.title, "zh-CN") || left.gameId.localeCompare(right.gameId);
}

export function filterLibraryGames(games: GameSummary[], filters: LibraryFilters) {
  const query = filters.query.trim().toLocaleLowerCase("zh-CN");
  return games.filter((game) => {
    if (filters.platformId && game.platform.id !== filters.platformId) return false;
    if (filters.platformInstanceId && game.platformInstance.id !== filters.platformInstanceId) return false;
    if (!query) return true;
    return [game.title, game.platform.name, game.platformInstance.name, game.defaultCore.name]
      .some((value) => value.toLocaleLowerCase("zh-CN").includes(query));
  }).sort((left, right) => {
    if (filters.sort === "TITLE_ASC") return stableTitleOrder(left, right);
    if (filters.sort === "ADDED_DESC") return right.createdAtMs - left.createdAtMs || stableTitleOrder(left, right);
    const leftPlayed = left.lastPlayedAtMs ?? Number.NEGATIVE_INFINITY;
    const rightPlayed = right.lastPlayedAtMs ?? Number.NEGATIVE_INFINITY;
    return rightPlayed - leftPlayed || right.createdAtMs - left.createdAtMs || stableTitleOrder(left, right);
  });
}

export function libraryPlatforms(games: GameSummary[]) {
  const platforms = new Map<string, { id: string; name: string; count: number }>();
  for (const game of games) {
    const current = platforms.get(game.platform.id);
    if (current) current.count += 1;
    else platforms.set(game.platform.id, { ...game.platform, count: 1 });
  }
  return [...platforms.values()].sort((left, right) =>
    left.name.localeCompare(right.name, "zh-CN") || left.id.localeCompare(right.id));
}

export function libraryPlatformInstances(games: GameSummary[], platformId: string) {
  const collections = new Map<string, { id: string; name: string; platformId: string }>();
  for (const game of games) {
    if (platformId && game.platform.id !== platformId) continue;
    collections.set(game.platformInstance.id, { ...game.platformInstance, platformId: game.platform.id });
  }
  return [...collections.values()].sort((left, right) =>
    left.name.localeCompare(right.name, "zh-CN") || left.id.localeCompare(right.id));
}

function sameLocalDay(left: Date, right: Date) {
  return left.getFullYear() === right.getFullYear() && left.getMonth() === right.getMonth() && left.getDate() === right.getDate();
}

export function formatLibraryPlayedAt(value: number | null, nowMs: number) {
  if (value === null) return "尚未游玩";
  const date = new Date(value);
  const now = new Date(nowMs);
  const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1);
  const prefix = sameLocalDay(date, now)
    ? "今天"
    : sameLocalDay(date, yesterday)
      ? "昨天"
      : `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`;
  return `${prefix} ${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
}
