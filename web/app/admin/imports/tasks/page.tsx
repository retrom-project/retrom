import { ButtonLink, EmptyState, PageHeader } from "@/components/ui";
import { ImportTaskBoard } from "@/features/imports/import-task-board";
import type { ImportListItem } from "@/features/imports/import-workflow";
import { backendJSON, scalarSearchParams, type ListResponse } from "@/lib/backend";

export const metadata = { title: "任务进度" };

export default async function ImportTasksPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "state"]);
  const imports = await backendJSON<ListResponse<ImportListItem>>("/api/v1/admin/imports?limit=20");
  return <div className="import-workflow-page import-tasks-page"><PageHeader eyebrow="游戏入库" title="任务进度" description="关注每个导入批次当前所处阶段、完成比例和条目分布。异常和运行中任务优先展示。" actions={<ButtonLink href="/admin/imports/new">＋ 新建导入</ButtonLink>} />{imports.items.length === 0 ? <EmptyState title="还没有入库任务" description="选择并上传游戏内容后，任务进度会显示在这里。" /> : <ImportTaskBoard initial={imports} initialQuery={values.q} initialState={values.state === "FAILED" ? "ATTENTION" : values.state} />}</div>;
}
