import { ButtonLink, PageHeader } from "@/components/ui";
import { BIOSManager, type BIOSListResponse } from "@/features/bios/bios-manager";
import { RuntimeTargetDiagnostics, type RuntimeTargetList } from "@/features/bios/runtime-target-diagnostics";
import { scalarSearchParams } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "运行依赖" };

export default async function BIOSPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "coreId", "scope", "status", "quick"]);
  const scope = values.scope === "FULL_CATALOG" ? "FULL_CATALOG" : "REQUIRED_BY_LIBRARY";
  const quick = values.quick === "ATTENTION" || values.quick === "REQUIRED" || values.quick === "OPTIONAL" ? values.quick : "ALL";
  const query = new URLSearchParams({ scope, limit: "100", quick });
  for (const key of ["q", "coreId", "status"] as const) {if (values[key]) {query.set(key, values[key]);}}
  const [initialResponse, runtimeTargets] = await Promise.all([
    backendJSON<BIOSListResponse>(`/api/v1/admin/bios?${query.toString()}`),
    backendJSON<RuntimeTargetList>("/api/v1/admin/runtime-targets"),
  ]);
  return <div className="page-layout page-layout-admin runtime-dependency-shell">
    <PageHeader eyebrow="管理后台" title="运行依赖" description="管理模拟器所需的 BIOS 文件与归档，查看缺失状态并安装。" actions={<ButtonLink href="/admin/imports/server?action=bios" secondary>服务器批量导入 BIOS</ButtonLink>} />
    <BIOSManager initialResponse={initialResponse} initialScope={scope} initialFilters={{ query: values.q ?? "", coreId: values.coreId ?? "", status: values.status ?? "", quick }} />
    <details><summary>RPG Maker 核心诊断</summary><RuntimeTargetDiagnostics catalog={runtimeTargets} /></details>
  </div>;
}
