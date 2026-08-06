import { ButtonLink, EmptyState, PageHeader } from "@/components/ui";
import { ListFilters } from "@/components/list-filters";
import { BIOSManager, type BIOSRequirement } from "@/features/bios/bios-manager";
import { backendJSON, scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";

export const metadata = { title: "BIOS 管理" };

export default async function BIOSPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["coreId", "scope", "status"]);
  const bios = await backendJSON<ListResponse<BIOSRequirement>>(withQuery("/api/v1/admin/bios", values));
  return <><PageHeader title="BIOS 管理" description="默认聚焦当前资料库实际需要的 BIOS；安装会保留旧 revision 与校验证据。" actions={<ButtonLink href="/admin/bios/dats" secondary>Arcade DAT 版本</ButtonLink>} /><ListFilters action="/admin/bios" placeholder="按核心筛选 BIOS…" values={values} filters={[{ name: "scope", label: "需求范围", options: [{ value: "REQUIRED_BY_LIBRARY", label: "资料库所需" }, { value: "FULL_CATALOG", label: "完整核心目录" }] }, { name: "status", label: "BIOS 状态", options: [{ value: "", label: "所有状态" }, { value: "MISSING", label: "缺失" }, { value: "MATCHED", label: "已匹配" }, { value: "HASH_WARNING", label: "Hash 警告" }] }]} />{bios.items.length === 0 ? <EmptyState title="当前没有 BIOS 需求" description="需求目录会根据启用核心、CoreArtifact 和活动 DAT 物化，不需要手工猜测。" /> : <BIOSManager items={bios.items} />}</>;
}
