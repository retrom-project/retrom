import type { TagReference } from "@/components/tag-picker";
import { backendJSON } from "@/lib/server-backend";
import { withQuery } from "@/lib/backend";

type TagPage = { items: Array<TagReference & { status: string }>; nextCursor: string | null };

export async function loadActiveTags() {
  const result: TagReference[] = [];
  let cursor: string | null = null;
  do {
    const page: TagPage = await backendJSON<TagPage>(withQuery("/api/v1/admin/tags", {
      status: "ACTIVE", sort: "NAME_ASC", limit: "100", ...(cursor ? { cursor } : {}),
    }));
    result.push(...page.items.map(({ tagId, name }) => ({ tagId, name })));
    cursor = page.nextCursor;
  } while (cursor);
  return result;
}
