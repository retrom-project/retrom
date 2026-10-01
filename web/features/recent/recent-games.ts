export type RecentGame = {
  gameId: string;
  title: string;
  status: "PUBLISHED" | "DELETED";
  availability: "PUBLISHED" | "DELETED";
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

export type RecentPage = {
  generatedAtMs: number;
  items: RecentGame[];
  nextCursor: string | null;
  filteredCount?: number;
  stats?: { gameCount: number; activeDurationMs: number; sessionCount: number };
  platforms?: Array<{ id: string; name: string }>;
};

export function recentGameURL(filters: RecentGameFilters) {
  const query = new URLSearchParams({ limit: "50", sort: {
    recent: "RECENT_DESC", title: "TITLE_ASC", duration: "DURATION_DESC", sessions: "SESSIONS_DESC",
  }[filters.sort] });
  if (filters.query.trim()) {query.set("q", filters.query.trim());}
  if (filters.platformId) {query.set("platformId", filters.platformId);}
  if (filters.period !== "all") {
    const days = filters.period === "7d" ? 7 : 30;
    query.set("fromAtMs", String(Math.max(0, filters.nowMs - days * 24 * 60 * 60 * 1000)));
  }
  return `/api/v1/recent-games?${query}`;
}

export function zonedDateKey(value: number, timeZone?: string) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    year: "numeric", month: "2-digit", day: "2-digit", timeZone,
  }).formatToParts(new Date(value));
  const values = Object.fromEntries(parts.map((part) => [part.type, part.value]));
  return `${values.year}-${values.month}-${values.day}`;
}

export function formatRecentDuration(value: number) {
  if (value < 60_000) {return "少于 1 分钟";}
  const minutes = Math.floor(value / 60_000);
  if (minutes < 60) {return `${minutes} 分钟`;}
  const hours = Math.floor(minutes / 60);
  const remainder = minutes % 60;
  return remainder === 0 ? `${hours} 小时` : `${hours} 小时 ${remainder} 分`;
}

export function formatRecentTime(value: number, timeZone?: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
    timeZone,
  }).format(new Date(value));
}
