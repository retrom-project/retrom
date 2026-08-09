import { ButtonLink, PageHeader } from "@/components/ui";
import { BIOSManager, type BIOSRequirement } from "@/features/bios/bios-manager";
import { scalarSearchParams, type ListResponse } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "BIOS 文件" };

export default async function BIOSPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "coreId", "scope", "status"]);
  const [library, catalog] = await Promise.all([
    backendJSON<ListResponse<BIOSRequirement>>("/api/v1/admin/bios?scope=REQUIRED_BY_LIBRARY"),
    backendJSON<ListResponse<BIOSRequirement>>("/api/v1/admin/bios?scope=FULL_CATALOG"),
  ]);
  const scope = values.scope === "FULL_CATALOG" ? "FULL_CATALOG" : "REQUIRED_BY_LIBRARY";
  return <div className="page-layout page-layout-admin runtime-dependency-shell">
    <PageHeader eyebrow="运行依赖" title="BIOS 文件" description="管理游戏运行所需的系统文件。默认只关注当前游戏库实际用到的依赖，也可切换到完整核心目录。" actions={<ButtonLink href="/admin/bios/dats" secondary>街机数据目录 →</ButtonLink>} />
    <BIOSManager libraryItems={library.items} catalogItems={catalog.items} initialScope={scope} initialFilters={{ query: values.q ?? "", coreId: values.coreId ?? "", status: values.status ?? "" }} />
  </div>;
}
