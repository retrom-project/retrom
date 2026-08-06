import Link from "next/link";
import { ListFilters } from "@/components/list-filters";
import { EmptyState, PageHeader, StatusBadge } from "@/components/ui";
import { backendJSON, formatTime, scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";

type Import = { id: string; state: string; platformInstanceName: string; metadataProvider: string; totalItemCount: number; reviewPendingItemCount: number; failedItemCount: number; version: number; updatedAtMs: number };

export const metadata = { title: "任务进度" };

export default async function ImportTasksPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "state"]);
  const imports = await backendJSON<ListResponse<Import>>(withQuery("/api/v1/admin/imports", values));
  return <><PageHeader title="任务进度" description="观察上传终结、分组、哈希、兼容性和元信息任务；离开页面不会取消后台任务。" /><ListFilters action="/admin/imports/tasks" placeholder="搜索任务或目标目录…" values={values} filters={[{ name: "state", label: "任务状态", options: [{ value: "", label: "所有状态" }, { value: "RUNNING", label: "运行中" }, { value: "REVIEW_PENDING", label: "待审核" }, { value: "FAILED", label: "失败" }, { value: "COMPLETED", label: "已完成" }] }]} />{imports.items.length === 0 ? <EmptyState title="还没有入库任务" description="完成一次上传验证并创建导入任务后，阶段事件会显示在这里。" /> : <section className="panel table-wrap"><table><thead><tr><th>任务</th><th>目标目录</th><th>状态</th><th>条目</th><th>待审核 / 失败</th><th>更新时间</th><th>操作</th></tr></thead><tbody>{imports.items.map((item) => <tr key={item.id}><td><strong>{item.id.slice(0, 13)}…</strong><small>{item.metadataProvider}</small></td><td>{item.platformInstanceName}</td><td><StatusBadge tone={item.state === "FAILED" ? "bad" : "info"}>{item.state}</StatusBadge></td><td>{item.totalItemCount}</td><td>{item.reviewPendingItemCount} / {item.failedItemCount}</td><td>{formatTime(item.updatedAtMs)}</td><td><Link className="row-action" href={`/admin/reviews?importJobId=${item.id}`}>查看</Link></td></tr>)}</tbody></table></section>}</>;
}
