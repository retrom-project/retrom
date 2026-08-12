import { ButtonLink, PageHeader } from "@/components/ui";
import { ServerImportManager, type ServerImportList, type ServerImportRoot } from "@/features/server-import/server-import-manager";
import type { BIOSListResponse } from "@/features/bios/bios-manager";
import type { PegasusImportList, PegasusPlatformInstance } from "@/features/server-import/pegasus-import-manager";
import type { ListResponse } from "@/lib/backend";
import { scalarSearchParams } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "服务器导入" };

export default async function ServerImportsPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["action"]);
  const [roots, imports, pegasusImports, bios, platformInstances] = await Promise.all([
    backendJSON<{ items: ServerImportRoot[] }>("/api/v1/admin/server-import-roots"),
    backendJSON<ServerImportList>("/api/v1/admin/server-imports?kind=BIOS_DIRECTORY&limit=10"),
    backendJSON<PegasusImportList>("/api/v1/admin/pegasus-imports?limit=10"),
    backendJSON<BIOSListResponse>("/api/v1/admin/bios?scope=FULL_CATALOG&limit=1"),
    backendJSON<ListResponse<PegasusPlatformInstance>>("/api/v1/admin/platform-instances"),
  ]);
  return <div className="page-layout page-layout-admin">
    <PageHeader eyebrow="服务器导入" title="从服务器目录导入" description="从部署管理员明确允许的只读位置异步发现内容，支持 BIOS 与 Pegasus ROM 目录。" actions={<><ButtonLink href="/admin/bios" secondary>BIOS 文件</ButtonLink><ButtonLink href="/admin/bios/dats" secondary>街机数据目录</ButtonLink></>} />
    <ServerImportManager initialRoots={roots.items} initialImports={imports} initialPegasusImports={pegasusImports} platformInstances={platformInstances.items} initialOpen={values.action === "bios"} initialPegasusOpen={values.action === "pegasus"} initialCatalogSummary={{ totalCount: bios.summary.totalCount, attentionCount: bios.summary.attentionCount }} />
  </div>;
}
