import { ButtonLink, EmptyState, PageHeader } from "@/components/ui";
import { ListFilters } from "@/components/list-filters";
import { BIOSManager, type BIOSRequirement } from "@/features/bios/bios-manager";
import { backendJSON, scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";

export const metadata = { title: "BIOS 管理" };

export default async function BIOSPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["coreId", "scope", "status"]);
  const bios = await backendJSON<ListResponse<BIOSRequirement>>(withQuery("/api/v1/admin/bios", values));
  return <><PageHeader title="BIOS 管理" description="查看游戏库当前需要的系统文件，并在缺失时按提示安装。" actions={<ButtonLink href="/admin/bios/dats" secondary>街机数据目录</ButtonLink>} /><ListFilters action="/admin/bios" placeholder="输入运行方式名称" values={values} resultCount={bios.items.length} filters={[{ name: "scope", label: "查看范围", options: [{ value: "REQUIRED_BY_LIBRARY", label: "游戏库当前需要" }, { value: "FULL_CATALOG", label: "查看全部" }] }, { name: "status", label: "文件状态", options: [{ value: "", label: "所有状态" }, { value: "MISSING", label: "需要安装" }, { value: "MATCHED", label: "已安装并匹配" }, { value: "HASH_WARNING", label: "文件需要核对" }] }]} />{bios.items.length === 0 ? <EmptyState title="当前没有 BIOS 需求" description="系统会根据现有游戏和运行方式自动整理需要的文件。" /> : <BIOSManager items={bios.items} />}</>;
}
