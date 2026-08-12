import { notFound } from "next/navigation";
import { ButtonLink, PageHeader } from "@/components/ui";
import { PegasusImportDetailManager, type PegasusCollection, type PegasusImportSummary, type PegasusItemList, type PegasusPlatformInstance } from "@/features/server-import/pegasus-import-manager";
import type { ServerImportRoot } from "@/features/server-import/server-import-manager";
import { type ListResponse, scalarSearchParams } from "@/lib/backend";
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
  let loaded: [PegasusImportSummary, PegasusItemList, PegasusCollection[], { items: ServerImportRoot[] }, ListResponse<PegasusPlatformInstance>];
  try {
    loaded = await Promise.all([
      backendJSON<PegasusImportSummary>(`/api/v1/admin/pegasus-imports/${encodeURIComponent(pegasusImportId)}`),
      backendJSON<PegasusItemList>(`/api/v1/admin/pegasus-imports/${encodeURIComponent(pegasusImportId)}/items?${itemQuery.toString()}`),
      loadCollections(pegasusImportId),
      backendJSON<{ items: ServerImportRoot[] }>("/api/v1/admin/server-import-roots"),
      backendJSON<ListResponse<PegasusPlatformInstance>>("/api/v1/admin/platform-instances"),
    ]);
  } catch (error) {
    if (error instanceof Error && error.message.includes("returned 404")) notFound();
    throw error;
  }
  const [summary, items, collections, roots, platformInstances] = loaded;
  return <div className="page-layout page-layout-admin"><PageHeader eyebrow="服务器导入 / Pegasus ROM" title="Pegasus 准备任务" description="查看来源准备、运行检查与审核进度。后台不会自动发布游戏，所有候选都由管理员逐项决定。" actions={<ButtonLink href="/admin/imports/server" secondary>← 返回导入历史</ButtonLink>} /><PegasusImportDetailManager initialSummary={summary} initialItems={items} collections={collections} roots={roots.items} platformInstances={platformInstances.items} initialFilters={{ query: filters.q ?? "", outcome: filters.outcome ?? "", warning: filters.warning ?? "", collectionId: filters.collectionId ?? "" }} /></div>;
}
