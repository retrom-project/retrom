import { ButtonLink, EmptyState, PageHeader } from "@/components/ui";
import { ImportTaskBoard } from "@/features/imports/import-task-board";
import type { ImportListItem } from "@/features/imports/import-workflow";
import { scalarSearchParams, type ListResponse } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "任务进度" };

export default async function ImportTasksPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "state"]);
  const imports = await backendJSON<ListResponse<ImportListItem>>("/api/v1/admin/imports?limit=20");
  return <div className="import-workflow-page import-tasks-page"><PageHeader eyebrow="游戏入库" title="普通任务进度" description="查看浏览器上传或重新配置产生的导入批次；Pegasus 目录按顶层批次统一显示在本地扫描。" actions={<><ButtonLink href="/admin/imports/server" secondary>本地扫描任务</ButtonLink><ButtonLink href="/admin/imports/new">＋ 新建导入</ButtonLink></>} />{imports.items.length === 0 ? <EmptyState title="还没有普通入库任务" description="选择并上传游戏内容后，任务进度会显示在这里；Pegasus 导入请前往本地扫描。" /> : <ImportTaskBoard initial={imports} initialQuery={values.q} initialState={values.state === "FAILED" ? "ATTENTION" : values.state} />}</div>;
}
