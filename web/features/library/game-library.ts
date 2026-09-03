import type { FavoriteReference } from "@/features/favorites/favorite-api";

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
  favorite: FavoriteReference | null;
  tags?: Array<{ tagId: string; name: string }>;
};

export type LibraryFilters = {
  query: string;
  platformId: string;
  platformInstanceId: string;
  tagId?: string;
  sort: "RECENT_DESC" | "ADDED_DESC" | "TITLE_ASC";
};

export type GamePage = {
  generatedAtMs: number;
  items: GameSummary[];
  nextCursor: string | null;
  filteredCount?: number;
  facets?: LibraryFacets;
};

export type LibraryFacet = { id: string; name: string; count: number; platformId?: string };

export type LibraryFacets = {
  totalCount: number;
  platforms: LibraryFacet[];
  platformInstances: LibraryFacet[];
  tags: LibraryFacet[];
};

export function gamePageQuery(filters: LibraryFilters, cursor?: string | null) {
  const query = new URLSearchParams({ limit: "50", sort: filters.sort });
  if (filters.query.trim()) {query.set("q", filters.query.trim());}
  if (filters.platformId) {query.set("platformId", filters.platformId);}
  if (filters.platformInstanceId) {query.set("platformInstanceId", filters.platformInstanceId);}
  if (filters.tagId) {query.set("tagId", filters.tagId);}
  if (cursor) {query.set("cursor", cursor);}
  return query.toString();
}

function stableTitleOrder(left: GameSummary, right: GameSummary) {
  return left.title.localeCompare(right.title, "zh-CN") || left.gameId.localeCompare(right.gameId);
}

export function filterLibraryGames(games: GameSummary[], filters: LibraryFilters) {
  const query = filters.query.trim().toLocaleLowerCase("zh-CN");
  return games
    .filter((game) => matchesLibraryFilters(game, filters, query))
    .sort((left, right) => compareLibraryGames(left, right, filters.sort));
}

function matchesLibraryFilters(game: GameSummary, filters: LibraryFilters, query: string) {
  if (filters.platformId && game.platform.id !== filters.platformId) {return false;}
  if (filters.platformInstanceId && game.platformInstance.id !== filters.platformInstanceId) {return false;}
  if (filters.tagId && !(game.tags ?? []).some((tag) => tag.tagId === filters.tagId)) {return false;}
  if (!query) {return true;}
  const searchable = [
    game.title,
    game.platform.name,
    game.platformInstance.name,
    game.defaultCore.name,
    ...(game.tags ?? []).map((tag) => tag.name),
  ];
  return searchable.some((value) => value.toLocaleLowerCase("zh-CN").includes(query));
}

function compareLibraryGames(
  left: GameSummary,
  right: GameSummary,
  sort: LibraryFilters["sort"],
) {
  if (sort === "TITLE_ASC") {return stableTitleOrder(left, right);}
  if (sort === "ADDED_DESC") {return right.createdAtMs - left.createdAtMs || stableTitleOrder(left, right);}
  const leftPlayed = left.lastPlayedAtMs ?? Number.NEGATIVE_INFINITY;
  const rightPlayed = right.lastPlayedAtMs ?? Number.NEGATIVE_INFINITY;
  return rightPlayed - leftPlayed || right.createdAtMs - left.createdAtMs || stableTitleOrder(left, right);
}

export function libraryTags(games: GameSummary[]) {
  const values = new Map<string, { tagId: string; name: string; count: number }>();
  for (const game of games) {for (const tag of game.tags ?? []) {
    const current = values.get(tag.tagId);
    if (current) {current.count += 1;}
    else {values.set(tag.tagId, { ...tag, count: 1 });}
  }}
  return [...values.values()].sort((left, right) => left.name.localeCompare(right.name, "zh-CN") || left.tagId.localeCompare(right.tagId));
}

export function libraryPlatforms(games: GameSummary[]) {
  const platforms = new Map<string, { id: string; name: string; count: number }>();
  for (const game of games) {
    const current = platforms.get(game.platform.id);
    if (current) {current.count += 1;}
    else {platforms.set(game.platform.id, { ...game.platform, count: 1 });}
  }
  return [...platforms.values()].sort((left, right) =>
    left.name.localeCompare(right.name, "zh-CN") || left.id.localeCompare(right.id));
}

export function libraryPlatformInstances(games: GameSummary[], platformId: string) {
  const collections = new Map<string, { id: string; name: string; platformId: string }>();
  for (const game of games) {
    if (platformId && game.platform.id !== platformId) {continue;}
    collections.set(game.platformInstance.id, { ...game.platformInstance, platformId: game.platform.id });
  }
  return [...collections.values()].sort((left, right) =>
    left.name.localeCompare(right.name, "zh-CN") || left.id.localeCompare(right.id));
}

function sameLocalDay(left: Date, right: Date) {
  return left.getFullYear() === right.getFullYear() && left.getMonth() === right.getMonth() && left.getDate() === right.getDate();
}

function zonedParts(value: number, timeZone: string) {
  const values = Object.fromEntries(new Intl.DateTimeFormat("en-CA", {
    timeZone, year: "numeric", month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit", hourCycle: "h23",
  }).formatToParts(value).map((part) => [part.type, part.value]));
  return {
    year: Number(values.year), month: Number(values.month), day: Number(values.day),
    hours: values.hour, minutes: values.minute,
  };
}

export function formatLibraryPlayedAt(value: number | null, nowMs: number, timeZone?: string) {
  if (value === null) {return "尚未游玩";}
  if (timeZone) {
    const date = zonedParts(value, timeZone);
    const now = zonedParts(nowMs, timeZone);
    const previous = new Date(Date.UTC(now.year, now.month - 1, now.day - 1));
    const yesterday = { year: previous.getUTCFullYear(), month: previous.getUTCMonth() + 1, day: previous.getUTCDate() };
    const sameDay = date.year === now.year && date.month === now.month && date.day === now.day;
    const previousDay = date.year === yesterday.year && date.month === yesterday.month && date.day === yesterday.day;
    const prefix = sameDay ? "今天" : previousDay ? "昨天" : `${date.year}/${date.month}/${date.day}`;
    return `${prefix} ${date.hours}:${date.minutes}`;
  }
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
