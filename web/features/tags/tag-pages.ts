import { responseError } from "@/lib/upload";
import type { TagAdminItem, TagAdminPage } from "./tag-manager";

export function appendTagPage(current: TagAdminItem[], incoming: TagAdminItem[]) {
  const seen = new Set(current.map((item) => item.tagId));
  return [...current, ...incoming.filter((item) => !seen.has(item.tagId))];
}

// A mutation invalidates cursor positions as well as the visible order/filter.
// Re-read the loaded range from the server, whose collation owns this contract.
export async function readTagRange(filters: { q: string; status: string; sort: string }, count: number): Promise<TagAdminPage> {
  let cursor: string | null = null;
  let items: TagAdminItem[] = [];
  do {
    const query = new URLSearchParams({ ...filters, limit: "100", ...(cursor ? { cursor } : {}) });
    const response = await fetch(`/api/v1/admin/tags?${query}`, { cache: "no-store" });
    if (!response.ok) {throw new Error(await responseError(response, "读取标签列表失败"));}
    const page = await response.json() as TagAdminPage;
    items = appendTagPage(items, page.items);
    cursor = page.nextCursor;
    if (!cursor || items.length >= count) {return { ...page, items };}
  } while (cursor);
  throw new Error("读取标签列表失败");
}
