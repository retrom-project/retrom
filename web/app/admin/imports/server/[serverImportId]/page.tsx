import { notFound } from "next/navigation";
import { ButtonLink, PageHeader } from "@/components/ui";
import { ServerImportDetailManager, type ServerImportDetail } from "@/features/server-import/server-import-manager";
import { backendJSON } from "@/lib/server-backend";
import { scalarSearchParams } from "@/lib/backend";

export const metadata = { title: "服务器导入详情" };

export default async function ServerImportDetailPage({ params, searchParams }: { params: Promise<{ serverImportId: string }>; searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const { serverImportId } = await params;
  const filters = scalarSearchParams(await searchParams, ["q", "outcome", "matchMethod"]);
  const query = new URLSearchParams({ limit: "50" });
  for (const key of ["q", "outcome", "matchMethod"] as const) if (filters[key]) query.set(key, filters[key]);
  let detail: ServerImportDetail;
  try {
    detail = await backendJSON<ServerImportDetail>(`/api/v1/admin/server-imports/${encodeURIComponent(serverImportId)}?${query.toString()}`);
  } catch (error) {
    if (error instanceof Error && error.message.includes("returned 404")) notFound();
    throw error;
  }
  return <div className="page-layout page-layout-admin">
    <PageHeader eyebrow="服务器导入" title="BIOS 导入详情" description="实时进度、逐项结果和候选排序证据会在刷新或重新进入后恢复。" actions={<ButtonLink href="/admin/imports/server" secondary>← 返回导入历史</ButtonLink>} />
    <ServerImportDetailManager initialDetail={detail} initialFilters={{ query: filters.q ?? "", outcome: filters.outcome ?? "", matchMethod: filters.matchMethod ?? "" }} />
  </div>;
}
