import { notFound } from "next/navigation";
import { ButtonLink, PageHeader } from "@/components/ui";
import { loadActiveTags } from "@/features/tags/tag-library";
import { EmulationStationImportDetailManager } from "@/features/server-import/emulationstation-import-detail-manager";
import type {
  EmulationStationCollection,
  EmulationStationGamelist,
  EmulationStationImportSummary,
  EmulationStationItemList,
  EmulationStationPlatformInstance,
} from "@/features/server-import/emulationstation-import-manager";
import type { ServerImportRoot } from "@/features/server-import/server-import-manager";
import { type ListResponse, scalarSearchParams } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "EmulationStation 导入详情" };

async function loadAll<T>(path: string) {
  const items: T[] = []; let cursor: string | null = null;
  do {
    const query = new URLSearchParams({ limit: "100" }); if (cursor) {query.set("cursor", cursor);}
    const page = await backendJSON<{ items: T[]; nextCursor: string | null }>(`${path}?${query.toString()}`);
    items.push(...page.items); cursor = page.nextCursor;
  } while (cursor);
  return items;
}

export default async function EmulationStationImportDetailPage({ params, searchParams }: {
  params: Promise<{ emulationStationImportId: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { emulationStationImportId } = await params;
  const filters = scalarSearchParams(await searchParams, ["q", "outcome", "warning", "collectionId"]);
  const query = new URLSearchParams({ limit: "50" });
  if (filters.q) {query.set("q", filters.q);} if (filters.outcome) {query.set("outcome", filters.outcome);}
  if (filters.warning) {query.set("warning", filters.warning);} if (filters.collectionId) {query.set("collectionId", filters.collectionId);}
  const base = `/api/v1/admin/emulationstation-imports/${encodeURIComponent(emulationStationImportId)}`;
  let loaded: [EmulationStationImportSummary, EmulationStationItemList, EmulationStationCollection[], EmulationStationGamelist[], { items: ServerImportRoot[] }, ListResponse<EmulationStationPlatformInstance>, Awaited<ReturnType<typeof loadActiveTags>>];
  try {
    loaded = await Promise.all([
      backendJSON<EmulationStationImportSummary>(base), backendJSON<EmulationStationItemList>(`${base}/items?${query.toString()}`),
      loadAll<EmulationStationCollection>(`${base}/collections`), loadAll<EmulationStationGamelist>(`${base}/gamelists`),
      backendJSON<{ items: ServerImportRoot[] }>("/api/v1/admin/server-import-roots"),
      backendJSON<ListResponse<EmulationStationPlatformInstance>>("/api/v1/admin/platform-instances"), loadActiveTags(),
    ]);
  } catch (error) {
    if (error instanceof Error && error.message.includes("returned 404")) {notFound();}
    throw error;
  }
  const [summary, items, collections, gamelists, roots, platformInstances, activeTags] = loaded;
  return <div className="page-layout page-layout-admin"><PageHeader eyebrow="服务器导入 / EmulationStation" title="Gamelist 准备任务" description="查看清单、来源提示、运行检查与审核进度。系统不会执行来源命令，也不会自动发布游戏。" actions={<ButtonLink href="/admin/imports/server" secondary>← 返回导入历史</ButtonLink>} /><EmulationStationImportDetailManager initialSummary={summary} initialItems={items} collections={collections} gamelists={gamelists} roots={roots.items} platformInstances={platformInstances.items} activeTags={activeTags} initialFilters={{ query: filters.q ?? "", outcome: filters.outcome ?? "", warning: filters.warning ?? "", collectionId: filters.collectionId ?? "" }} /></div>;
}
