import { ButtonLink, EmptyState, PageHeader } from "@/components/ui";
import { ListFilters } from "@/components/list-filters";
import { DATManager, type CoreArtifact, type DATVersion } from "@/features/bios/dat-manager";
import { backendJSON, scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";

export const metadata = { title: "Arcade DAT 版本" };

export default async function DATPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["coreId", "source", "parseStatus"]);
  const [versions, artifacts] = await Promise.all([
    backendJSON<ListResponse<DATVersion>>(withQuery("/api/v1/admin/arcade-dats", values)),
    backendJSON<ListResponse<CoreArtifact>>("/api/v1/admin/core-artifacts")
  ]);
  return <><PageHeader title="Arcade DAT 版本" description="DAT 与精确 CoreArtifact 绑定；候选解析、差异、启用和回滚都保留版本化证据。" actions={<ButtonLink href="/admin/bios" secondary>返回 BIOS</ButtonLink>} /><ListFilters action="/admin/bios/dats" placeholder="按核心筛选 DAT…" values={values} filters={[{ name: "source", label: "DAT 来源", options: [{ value: "", label: "所有来源" }, { value: "BUILTIN", label: "内置基线" }, { value: "USER", label: "用户上传" }] }, { name: "parseStatus", label: "解析状态", options: [{ value: "", label: "所有状态" }, { value: "PENDING", label: "待解析" }, { value: "PARSING", label: "解析中" }, { value: "READY", label: "可启用" }, { value: "FAILED", label: "失败" }, { value: "CANCELLED", label: "已取消" }] }]} />{versions.items.length === 0 ? <EmptyState title="尚无 DAT 版本" description="依赖准备和进程启动会验证并登记内置 Arcade DAT 基线。" /> : <DATManager versions={versions.items} artifacts={artifacts.items} />}</>;
}
