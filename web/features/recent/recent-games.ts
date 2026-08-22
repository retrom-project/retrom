export type RecentGame = {
  gameId: string;
  title: string;
  platform: { id: string; name: string };
  platformInstance: { id: string; name: string };
  lastPlayedAtMs: number;
  activeDurationMs: number;
  sessionCount: number;
  coverUrl: string | null;
  tags?: Array<{ tagId: string; name: string }>;
};

export type RecentGameFilters = {
  query: string;
  platformId: string;
  sort: "recent" | "title" | "duration" | "sessions";
  period: "all" | "7d" | "30d";
  nowMs: number;
};

const DAY_MS = 24 * 60 * 60 * 1000;

export function filterRecentGames(games: RecentGame[], filters: RecentGameFilters) {
  const query = filters.query.trim().toLocaleLowerCase("zh-CN");
  const periodDays = filters.period === "7d" ? 7 : filters.period === "30d" ? 30 : null;
  const cutoff = periodDays === null ? null : filters.nowMs - periodDays * DAY_MS;
  const result = games.filter((game) => {
    if (filters.platformId && game.platform.id !== filters.platformId) {return false;}
    if (cutoff !== null && game.lastPlayedAtMs < cutoff) {return false;}
    if (!query) {return true;}
    return [game.title, game.platform.name, game.platformInstance.name, ...(game.tags ?? []).map((tag) => tag.name)]
      .some((value) => value.toLocaleLowerCase("zh-CN").includes(query));
  });
  return result.sort((left, right) => {
    if (filters.sort === "title") {
      const titleOrder = left.title.localeCompare(right.title, "zh-CN", { sensitivity: "base" });
      return titleOrder || left.gameId.localeCompare(right.gameId);
    }
    if (filters.sort === "duration" && left.activeDurationMs !== right.activeDurationMs) {
      return right.activeDurationMs - left.activeDurationMs;
    }
    if (filters.sort === "sessions" && left.sessionCount !== right.sessionCount) {
      return right.sessionCount - left.sessionCount;
    }
    return right.lastPlayedAtMs - left.lastPlayedAtMs || right.gameId.localeCompare(left.gameId);
  });
}

export function recentGameStats(games: RecentGame[]) {
  return games.reduce((summary, game) => ({
    gameCount: summary.gameCount + 1,
    activeDurationMs: summary.activeDurationMs + game.activeDurationMs,
    sessionCount: summary.sessionCount + game.sessionCount,
  }), { gameCount: 0, activeDurationMs: 0, sessionCount: 0 });
}

export function startOfLocalDay(nowMs: number) {
  const date = new Date(nowMs);
  return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
}

export function formatRecentDuration(value: number) {
  if (value < 60_000) {return "少于 1 分钟";}
  const minutes = Math.floor(value / 60_000);
  if (minutes < 60) {return `${minutes} 分钟`;}
  const hours = Math.floor(minutes / 60);
  const remainder = minutes % 60;
  return remainder === 0 ? `${hours} 小时` : `${hours} 小时 ${remainder} 分`;
}

export function formatRecentTime(value: number) {
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(new Date(value));
}
