import { notFound } from "next/navigation";
import { ButtonLink, PageHeader } from "@/components/ui";
import { PegasusImportDetailManager, type PegasusCollection, type PegasusImportSummary, type PegasusItemList } from "@/features/server-import/pegasus-import-manager";
import { scalarSearchParams } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "Pegasus 导入详情" };

async function loadCollections(importId: string) {
  const items: PegasusCollection[] = [];
  let cursor: string | null = null;
  do {
    const query = new URLSearchParams({ limit: "100" });
    if (cursor) query.set("cursor", cursor);
    const page = await backendJSON<{ items: PegasusCollection[]; nextCursor: string | null }>(`/api/v1/admin/pegasus-imports/${encodeURIComponent(importId)}/collections?${query.toString()}`);
    items.push(...page.items);
    cursor = page.nextCursor;
  } while (cursor);
  return items;
}

export default async function PegasusImportDetailPage({ params, searchParams }: {
  params: Promise<{ pegasusImportId: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { pegasusImportId } = await params;
  const filters = scalarSearchParams(await searchParams, ["q", "outcome", "warning", "collectionId"]);
  const itemQuery = new URLSearchParams({ limit: "50" });
  if (filters.q) itemQuery.set("q", filters.q);
  if (filters.outcome) itemQuery.set("outcome", filters.outcome);
  if (filters.warning) itemQuery.set("warning", filters.warning);
  if (filters.collectionId) itemQuery.set("collectionId", filters.collectionId);
  let loaded: [PegasusImportSummary, PegasusItemList, PegasusCollection[]];
  try {
    loaded = await Promise.all([
      backendJSON<PegasusImportSummary>(`/api/v1/admin/pegasus-imports/${encodeURIComponent(pegasusImportId)}`),
      backendJSON<PegasusItemList>(`/api/v1/admin/pegasus-imports/${encodeURIComponent(pegasusImportId)}/items?${itemQuery.toString()}`),
      loadCollections(pegasusImportId),
    ]);
  } catch (error) {
    if (error instanceof Error && error.message.includes("returned 404")) notFound();
    throw error;
  }
  const [summary, items, collections] = loaded;
  return <div className="page-layout page-layout-admin"><PageHeader eyebrow="服务器导入 / Pegasus ROM" title="Pegasus 导入详情" description="查看映射、逐项发布、阻断与媒体警告；刷新或重新进入后会恢复当前任务进度。" actions={<ButtonLink href="/admin/imports/server" secondary>← 返回导入历史</ButtonLink>} /><PegasusImportDetailManager initialSummary={summary} initialItems={items} collections={collections} initialFilters={{ query: filters.q ?? "", outcome: filters.outcome ?? "", warning: filters.warning ?? "", collectionId: filters.collectionId ?? "" }} /></div>;
}
