import { ButtonLink, PageHeader } from "@/components/ui";
import type { BIOSListResponse } from "@/features/bios/bios-manager";
import { RuntimeDependenciesManager } from "@/features/bios/runtime-dependencies-manager";
import type {RuntimeAssetPackList, RuntimeTargetList} from "@/features/bios/runtime-asset-pack-manager";
import { scalarSearchParams } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "运行依赖" };

export default async function BIOSPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "coreId", "scope", "status", "quick", "tab"]);
  const scope = values.scope === "FULL_CATALOG" ? "FULL_CATALOG" : "REQUIRED_BY_LIBRARY";
  const quick = values.quick === "ATTENTION" || values.quick === "REQUIRED" || values.quick === "OPTIONAL" ? values.quick : "ALL";
  const query = new URLSearchParams({ scope, limit: "100", quick });
  for (const key of ["q", "coreId", "status"] as const) {if (values[key]) {query.set(key, values[key]);}}
  const [initialResponse, initialPackList, initialRuntimeTargets] = await Promise.all([
    backendJSON<BIOSListResponse>(`/api/v1/admin/bios?${query.toString()}`),
    backendJSON<RuntimeAssetPackList>("/api/v1/admin/runtime-asset-packs"),
    backendJSON<RuntimeTargetList>("/api/v1/admin/runtime-targets"),
  ]);
  return <div className="page-layout page-layout-admin runtime-dependency-shell">
    <PageHeader eyebrow="管理后台" title="运行依赖" description="分别管理模拟器 BIOS 与 RPG Maker 运行包；两类资源使用独立的校验、引用和删除规则。" actions={<ButtonLink href="/admin/imports/server?action=bios" secondary>服务器批量导入 BIOS</ButtonLink>} />
    <RuntimeDependenciesManager initialBIOS={initialResponse} initialPackList={initialPackList} initialRuntimeTargets={initialRuntimeTargets} initialTab={values.tab === "rpgmaker" ? "rpgmaker" : "bios"} initialScope={scope} initialFilters={{ query: values.q ?? "", coreId: values.coreId ?? "", status: values.status ?? "", quick }} />
  </div>;
}
