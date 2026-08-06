const backend = process.env.NEXT_BACKEND_ORIGIN ?? "http://127.0.0.1:8080";

export async function backendJSON<T>(path: string): Promise<T> {
  const response = await fetch(`${backend}${path}`, { cache: "no-store", headers: { Accept: "application/json" } });
  if (!response.ok) throw new Error(`Retrom API ${path} returned ${response.status}`);
  return response.json() as Promise<T>;
}

export type ListResponse<T> = { items: T[]; nextCursor: string | null };

export function scalarSearchParams(values: Record<string, string | string[] | undefined>, allowed: string[]) {
  const result: Record<string, string> = {};
  for (const name of allowed) {
    const value = values[name];
    if (typeof value === "string" && value) result[name] = value;
  }
  return result;
}

export function withQuery(path: string, values: Record<string, string>) {
  const query = new URLSearchParams(values).toString();
  return query ? `${path}?${query}` : path;
}

export function formatTime(value: number | null | undefined) {
  if (!value) return "尚无记录";
  return new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "short" }).format(new Date(value));
}
