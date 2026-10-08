import { ApiError } from "@/lib/api/client";

export const recommendedTags = [
  "动作冒险", "飞行射击", "格斗对战", "角色扮演", "模拟经营",
  "即时战略", "体育竞技", "益智解谜", "光枪射击", "生存恐怖",
] as const;

type TagPage = { items: Array<{ name: string }>; total: number };
export async function createRecommendedTags(actions: {
  list: (offset: number) => Promise<TagPage>;
  create: (name: string) => Promise<unknown>;
}, signal: AbortSignal) {
  const existing = new Set<string>();
  for (let offset = 0; !signal.aborted; offset += 100) {
    const page = await actions.list(offset);
    for (const tag of page.items) { existing.add(nameKey(tag.name)); }
    if (offset + 100 >= page.total || page.items.length < 100) { break; }
  }
  const summary = { created: 0, existing: 0, failures: [] as Array<{ name: string; message: string }> };
  for (const name of recommendedTags) {
    if (signal.aborted) { break; }
    if (existing.has(nameKey(name))) { summary.existing++; continue; }
    try {
      await actions.create(name);
      summary.created++;
    } catch (failure) {
      if (failure instanceof ApiError && failure.code === "TAG_NAME_CONFLICT") { summary.existing++; }
      else {
        summary.failures.push({ name, message: failure instanceof Error ? failure.message : "创建失败" });
        if (failure instanceof ApiError && [401, 403].includes(failure.status)) { break; }
      }
    }
  }
  return summary;
}

function nameKey(name: string) {
  return name.replace(/\p{White_Space}+/gu, " ").replace(/^ | $/g, "").toLowerCase();
}
