import { ButtonLink, PageHeader } from "@/components/ui";
import { ServerImportManager, type ServerImportList, type ServerImportRoot } from "@/features/server-import/server-import-manager";
import type { BIOSListResponse } from "@/features/bios/bios-manager";
import { scalarSearchParams } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "服务器导入" };

export default async function ServerImportsPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["action"]);
  const [roots, imports, bios] = await Promise.all([
    backendJSON<{ items: ServerImportRoot[] }>("/api/v1/admin/server-import-roots"),
    backendJSON<ServerImportList>("/api/v1/admin/server-imports?kind=BIOS_DIRECTORY&limit=10"),
    backendJSON<BIOSListResponse>("/api/v1/admin/bios?scope=FULL_CATALOG&limit=1"),
  ]);
  return <div className="page-layout page-layout-admin">
    <PageHeader eyebrow="服务器导入" title="从服务器目录导入" description="从部署管理员明确允许的只读位置异步发现内容。本期支持 BIOS，后续能力会沿用同一任务与证据模型。" actions={<><ButtonLink href="/admin/bios" secondary>BIOS 文件</ButtonLink><ButtonLink href="/admin/bios/dats" secondary>街机数据目录</ButtonLink></>} />
    <ServerImportManager initialRoots={roots.items} initialImports={imports} initialOpen={values.action === "bios"} initialCatalogSummary={{ totalCount: bios.summary.totalCount, attentionCount: bios.summary.attentionCount }} />
  </div>;
}
