import { notFound } from "next/navigation";
import { ButtonLink, PageHeader } from "@/components/ui";
import { SourceImportDetailManager, type SourceCollection, type SourceImportSummary, type SourceItemList, type SourcePlatformInstance } from "@/features/server-import/source-import-manager";
import type { ServerImportRoot } from "@/features/server-import/server-import-manager";
import { loadActiveTags } from "@/features/tags/tag-library";
import type { TagReference } from "@/components/tag-picker";
import { type ListResponse, scalarSearchParams } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "游戏导入详情" };

async function loadCollections(importId: string) {
  const items: SourceCollection[] = [];
  let cursor: string | null = null;
  do {
    const query = new URLSearchParams({ limit: "100" });
    if (cursor) {query.set("cursor", cursor);}
    const page = await backendJSON<{ items: SourceCollection[]; nextCursor: string | null }>(`/api/v1/admin/source-imports/${encodeURIComponent(importId)}/collections?${query.toString()}`);
    items.push(...page.items);
    cursor = page.nextCursor;
  } while (cursor);
  return items;
}

export default async function SourceImportDetailPage({ params, searchParams }: {
  params: Promise<{ sourceImportId: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { sourceImportId } = await params;
  const filters = scalarSearchParams(await searchParams, ["q", "outcome", "warning", "collectionId"]);
  const itemQuery = new URLSearchParams({ limit: "50" });
  if (filters.q) {itemQuery.set("q", filters.q);}
  if (filters.outcome) {itemQuery.set("outcome", filters.outcome);}
  if (filters.warning) {itemQuery.set("warning", filters.warning);}
  if (filters.collectionId) {itemQuery.set("collectionId", filters.collectionId);}
  let loaded: [SourceImportSummary, SourceItemList, SourceCollection[], { items: ServerImportRoot[] }, ListResponse<SourcePlatformInstance>, TagReference[]];
  try {
    loaded = await Promise.all([
      backendJSON<SourceImportSummary>(`/api/v1/admin/source-imports/${encodeURIComponent(sourceImportId)}`),
      backendJSON<SourceItemList>(`/api/v1/admin/source-imports/${encodeURIComponent(sourceImportId)}/items?${itemQuery.toString()}`),
      loadCollections(sourceImportId),
      backendJSON<{ items: ServerImportRoot[] }>("/api/v1/admin/server-import-roots"),
      backendJSON<ListResponse<SourcePlatformInstance>>("/api/v1/admin/platform-instances"),
      loadActiveTags(),
    ]);
  } catch (error) {
    if (error instanceof Error && error.message.includes("returned 404")) {notFound();}
    throw error;
  }
  const [summary, items, collections, roots, platformInstances, activeTags] = loaded;
  return <div className="page-layout page-layout-admin"><PageHeader title="来源准备任务" description="查看来源准备、运行检查与审核进度。后台不会自动发布游戏，所有候选都由管理员逐项决定。" actions={<ButtonLink href="/admin/imports/server" secondary>返回导入历史</ButtonLink>} /><SourceImportDetailManager initialSummary={summary} initialItems={items} collections={collections} roots={roots.items} platformInstances={platformInstances.items} activeTags={activeTags} initialFilters={{ query: filters.q ?? "", outcome: filters.outcome ?? "", warning: filters.warning ?? "", collectionId: filters.collectionId ?? "" }} /></div>;
}
