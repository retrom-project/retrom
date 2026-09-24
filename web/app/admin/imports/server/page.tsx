import { ButtonLink, PageHeader } from "@/components/ui";
import { ServerImportManager, type ServerImportList, type ServerImportRoot } from "@/features/server-import/server-import-manager";
import type { BIOSListResponse } from "@/features/bios/bios-manager";
import type { SourceImportList, SourcePlatformInstance } from "@/features/server-import/source-import-manager";
import type { ListResponse } from "@/lib/backend";
import { scalarSearchParams } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";
import { loadActiveTags } from "@/features/tags/tag-library";

export const metadata = { title: "服务器导入" };

export default async function ServerImportsPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["action"]);
  const [roots, imports, sourceImports, bios, platformInstances, activeTags] = await Promise.all([
    backendJSON<{ items: ServerImportRoot[] }>("/api/v1/admin/server-import-roots"),
    backendJSON<ServerImportList>("/api/v1/admin/server-imports?kind=BIOS_DIRECTORY&limit=10"),
    backendJSON<SourceImportList>("/api/v1/admin/source-imports?limit=10"),
    backendJSON<BIOSListResponse>("/api/v1/admin/bios?scope=FULL_CATALOG&limit=1"),
    backendJSON<ListResponse<SourcePlatformInstance>>("/api/v1/admin/platform-instances"),
    loadActiveTags(),
  ]);
  return <div className="page-layout page-layout-admin">
    <PageHeader title="从服务器目录导入" description="浏览服务器可读取的目录，支持 BIOS 和 Pegasus / gamelist.xml 游戏目录导入。" actions={<ButtonLink href="/admin/bios" secondary>BIOS 文件</ButtonLink>} />
    <ServerImportManager initialRoots={roots.items} initialImports={imports} initialSourceImports={sourceImports} platformInstances={platformInstances.items} activeTags={activeTags} initialOpen={values.action === "bios"} initialSourceOpen={values.action === "source"} initialCatalogSummary={{ totalCount: bios.summary.totalCount, attentionCount: bios.summary.attentionCount }} />
  </div>;
}
