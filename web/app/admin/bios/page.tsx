import { ButtonLink, PageHeader } from "@/components/ui";
import { BIOSManager, type BIOSListResponse } from "@/features/bios/bios-manager";
import { scalarSearchParams } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "BIOS 文件" };

export default async function BIOSPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "coreId", "scope", "status", "quick"]);
  const scope = values.scope === "FULL_CATALOG" ? "FULL_CATALOG" : "REQUIRED_BY_LIBRARY";
  const quick = values.quick === "ATTENTION" || values.quick === "REQUIRED" || values.quick === "OPTIONAL" ? values.quick : "ALL";
  const query = new URLSearchParams({ scope, limit: "100", quick });
  for (const key of ["q", "coreId", "status"] as const) {if (values[key]) {query.set(key, values[key]);}}
  const initialResponse = await backendJSON<BIOSListResponse>(`/api/v1/admin/bios?${query.toString()}`);
  return <div className="page-layout page-layout-admin runtime-dependency-shell">
    <PageHeader eyebrow="运行依赖" title="BIOS 文件" description="管理游戏运行所需的系统文件。Arcade DAT 随核心版本由系统自动准备，无需手动上传或切换。" actions={<ButtonLink href="/admin/imports/server?action=bios" secondary>服务器批量导入</ButtonLink>} />
    <BIOSManager initialResponse={initialResponse} initialScope={scope} initialFilters={{ query: values.q ?? "", coreId: values.coreId ?? "", status: values.status ?? "", quick }} />
  </div>;
}
