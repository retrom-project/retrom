export type SaveItem = {
  saveStateId: string;
  gameId: string;
  gameTitle: string;
  name: string;
  version: number;
  createdAtMs: number;
  activeDurationMs: number;
  discIndex?: number | null;
  discLabel?: string | null;
  screenshotUrl: string | null;
  core: { id: string; name: string };
  platform: { id: string; name: string };
  platformInstance: { id: string; name: string };
  availability: { status: "AVAILABLE" | "BLOCKED"; reasons: Array<{ code?: string; logicalName?: string }> };
  tags?: Array<{ tagId: string; name: string }>;
};

export type SaveFilters = {
  query: string;
  gameId: string;
  availability: "AVAILABLE" | "BLOCKED" | "ALL";
  sort: "CREATED_DESC" | "CREATED_ASC";
};

export type SaveGroup = {
  gameId: string;
  gameTitle: string;
  platform: SaveItem["platform"];
  coreNames: string[];
  latestCreatedAtMs: number;
  saves: SaveItem[];
};

export type SavePage = {
  generatedAtMs: number;
  items: SaveItem[];
  nextCursor: string | null;
};

export async function collectSavePages(loadPage: (cursor: string | null) => Promise<SavePage>) {
  const items: SaveItem[] = [];
  const seenCursors = new Set<string>();
  let cursor: string | null = null;
  let generatedAtMs: number | null = null;
  do {
    const page = await loadPage(cursor);
    generatedAtMs ??= page.generatedAtMs;
    items.push(...page.items);
    cursor = page.nextCursor;
    if (cursor && seenCursors.has(cursor)) {throw new Error("Retrom API returned a repeated save cursor");}
    if (cursor) {seenCursors.add(cursor);}
  } while (cursor);
  return { generatedAtMs: generatedAtMs ?? 0, items };
}

export function saveAvailable(save: SaveItem) {
  return save.availability.status !== "BLOCKED";
}

export function filterSaveItems(saves: SaveItem[], filters: SaveFilters) {
  const query = filters.query.trim().toLocaleLowerCase("zh-CN");
  return saves.filter((save) => {
    if (filters.gameId && save.gameId !== filters.gameId) {return false;}
    if (filters.availability !== "ALL" && save.availability.status !== filters.availability) {return false;}
    if (!query) {return true;}
    return [save.gameTitle, save.name, save.core.name, save.platform.name, save.platformInstance.name, ...(save.tags ?? []).map((tag) => tag.name)]
      .some((value) => value.toLocaleLowerCase("zh-CN").includes(query));
  }).sort((left, right) => {
    const direction = filters.sort === "CREATED_ASC" ? 1 : -1;
    return direction * (left.createdAtMs - right.createdAtMs || left.saveStateId.localeCompare(right.saveStateId));
  });
}

export function groupSaveItems(saves: SaveItem[]) {
  const groups = new Map<string, SaveGroup>();
  for (const save of saves) {
    const group = groups.get(save.gameId);
    if (group) {
      group.saves.push(save);
      group.latestCreatedAtMs = Math.max(group.latestCreatedAtMs, save.createdAtMs);
      if (!group.coreNames.includes(save.core.name)) {group.coreNames.push(save.core.name);}
      continue;
    }
    groups.set(save.gameId, {
      gameId: save.gameId,
      gameTitle: save.gameTitle,
      platform: save.platform,
      coreNames: [save.core.name],
      latestCreatedAtMs: save.createdAtMs,
      saves: [save],
    });
  }
  return [...groups.values()];
}

export function saveLibraryStats(saves: SaveItem[]) {
  return { saveCount: saves.length, gameCount: new Set(saves.map((save) => save.gameId)).size };
}

export function latestAvailableSave(saves: SaveItem[]) {
  return saves.filter(saveAvailable).sort((left, right) =>
    right.createdAtMs - left.createdAtMs || right.saveStateId.localeCompare(left.saveStateId))[0] ?? null;
}

export function formatSaveDuration(value: number) {
  if (value < 60_000) {return "少于 1 分钟";}
  const minutes = Math.floor(value / 60_000);
  if (minutes < 60) {return `${minutes} 分钟`;}
  const hours = Math.floor(minutes / 60);
  const remainder = minutes % 60;
  return remainder === 0 ? `${hours} 小时` : `${hours} 小时 ${remainder} 分`;
}

function sameLocalDay(left: Date, right: Date) {
  return left.getFullYear() === right.getFullYear() && left.getMonth() === right.getMonth() && left.getDate() === right.getDate();
}

export function formatSaveTime(value: number, nowMs: number, includeSeconds = true) {
  const date = new Date(value);
  const now = new Date(nowMs);
  const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1);
  const prefix = sameLocalDay(date, now)
    ? "今天"
    : sameLocalDay(date, yesterday)
      ? "昨天"
      : `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`;
  const time = [date.getHours(), date.getMinutes(), ...(includeSeconds ? [date.getSeconds()] : [])]
    .map((part) => String(part).padStart(2, "0")).join(":");
  return `${prefix} ${time}`;
}

export function customSaveName(name: string) {
  const normalized = name.trim();
  return /^(手动存档|manual save)(?:\s|$)/i.test(normalized) ? null : normalized;
}
