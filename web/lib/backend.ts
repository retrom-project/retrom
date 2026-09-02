export type ListResponse<T> = { items: T[]; nextCursor: string | null };

export function scalarSearchParams(values: Record<string, string | string[] | undefined>, allowed: string[]) {
  const result: Record<string, string> = {};
  for (const name of allowed) {
    const value = values[name];
    if (typeof value === "string" && value) {result[name] = value;}
  }
  return result;
}

export function withQuery(path: string, values: Record<string, string>) {
  const query = new URLSearchParams(values).toString();
  return query ? `${path}?${query}` : path;
}

export function formatTime(value: number | null | undefined, timeZone?: string) {
  if (!value) {return "尚无记录";}
  return new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "short", timeZone }).format(new Date(value));
}

export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value < 0) {return "—";}
  if (value < 1024) {return `${value} B`;}
  const units = ["KB", "MB", "GB", "TB"];
  let amount = value;
  let unit = -1;
  do {
    amount /= 1024;
    unit += 1;
  } while (amount >= 1024 && unit < units.length - 1);
  const digits = amount >= 10 ? 1 : 2;
  return `${Number(amount.toFixed(digits))} ${units[unit]}`;
}
