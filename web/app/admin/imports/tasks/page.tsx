import Link from "next/link";
import { ListFilters } from "@/components/list-filters";
import { EmptyState, PageHeader, StatusBadge } from "@/components/ui";
import { backendJSON, formatTime, scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";

type Import = { id: string; state: string; platformInstanceName: string; metadataProvider: string; totalItemCount: number; reviewPendingItemCount: number; failedItemCount: number; version: number; updatedAtMs: number };

const stateLabels: Record<string, string> = { RUNNING: "运行中", REVIEW_PENDING: "等待审核", FAILED: "需要处理", COMPLETED: "已完成", CANCELLED: "已取消" };
const providerLabels: Record<string, string> = { HASHEOUS: "在线游戏信息", NONE: "不查找游戏信息" };

export const metadata = { title: "任务进度" };

export default async function ImportTasksPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "state"]);
  const imports = await backendJSON<ListResponse<Import>>(withQuery("/api/v1/admin/imports", values));
  return <><PageHeader title="任务进度" description="查看每次导入的当前进度和需要处理的问题；离开页面不会中断后台工作。" /><ListFilters action="/admin/imports/tasks" placeholder="搜索任务或目标目录…" values={values} filters={[{ name: "state", label: "任务状态", options: [{ value: "", label: "所有状态" }, { value: "RUNNING", label: "运行中" }, { value: "REVIEW_PENDING", label: "待审核" }, { value: "FAILED", label: "失败" }, { value: "COMPLETED", label: "已完成" }] }]} />{imports.items.length === 0 ? <EmptyState title="还没有入库任务" description="选择并上传游戏内容后，任务进度会显示在这里。" /> : <section className="panel table-wrap"><table><thead><tr><th>导入批次</th><th>目标目录</th><th>状态</th><th>游戏条目</th><th>待审核 / 异常</th><th>更新时间</th><th>操作</th></tr></thead><tbody>{imports.items.map((item) => <tr key={item.id}><td><strong>{formatTime(item.updatedAtMs)}</strong><small>{providerLabels[item.metadataProvider] ?? "游戏信息服务"}</small><details className="technical-details"><summary>技术详情</summary><code>{item.id}</code></details></td><td>{item.platformInstanceName}</td><td><StatusBadge tone={item.state === "FAILED" ? "bad" : item.state === "COMPLETED" ? "good" : "info"}>{stateLabels[item.state] ?? item.state}</StatusBadge></td><td>{item.totalItemCount}</td><td>{item.reviewPendingItemCount} / {item.failedItemCount}</td><td>{formatTime(item.updatedAtMs)}</td><td>{item.reviewPendingItemCount ? <Link className="row-action" href={`/admin/reviews?importJobId=${item.id}`}>查看待审核</Link> : <span className="muted-copy">无需审核</span>}</td></tr>)}</tbody></table></section>}</>;
}
