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
  return <><PageHeader title="街机数据目录" description="比较、启用或恢复街机游戏识别目录；每次更改前都会展示影响。" actions={<ButtonLink href="/admin/bios" secondary>返回系统文件</ButtonLink>} /><ListFilters action="/admin/bios/dats" placeholder="输入运行方式名称" values={values} resultCount={versions.items.length} filters={[{ name: "source", label: "目录来源", options: [{ value: "", label: "所有来源" }, { value: "BUILTIN", label: "系统内置" }, { value: "USER", label: "手动上传" }] }, { name: "parseStatus", label: "处理状态", options: [{ value: "", label: "所有状态" }, { value: "PENDING", label: "等待处理" }, { value: "PARSING", label: "正在处理" }, { value: "READY", label: "可以启用" }, { value: "FAILED", label: "需要处理" }, { value: "CANCELLED", label: "已取消" }] }]} />{versions.items.length === 0 ? <EmptyState title="尚无街机数据目录" description="依赖准备和进程启动会验证并登记系统内置基线。" /> : <DATManager versions={versions.items} artifacts={artifacts.items} />}</>;
}
