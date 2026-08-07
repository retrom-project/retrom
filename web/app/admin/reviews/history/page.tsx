import { ListFilters } from "@/components/list-filters";
import { EmptyState, PageHeader, StatusBadge } from "@/components/ui";
import { backendJSON, formatTime, scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";

type HistoryItem = { reviewEventId: string; importItemId: string; importJobId: string; title: string; decision: "APPROVED" | "DISCARDED"; reason: string | null; createdAtMs: number };

export const metadata = { title: "审核历史" };

export default async function ReviewHistoryPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "decision"]);
  const history = await backendJSON<ListResponse<HistoryItem>>(withQuery("/api/v1/admin/review-history", values));
  return <><PageHeader title="审核历史" description="查看已经发布或丢弃的游戏，以及当时采用的信息和决定。" /><ListFilters action="/admin/reviews/history" placeholder="搜索标题或来源…" values={values} filters={[{ name: "decision", label: "审核决定", options: [{ value: "", label: "所有决定" }, { value: "APPROVED", label: "已发布" }, { value: "DISCARDED", label: "已丢弃" }] }]} />{history.items.length === 0 ? <EmptyState title="尚无审核历史" description="通过或丢弃条目后，记录会显示在这里。" /> : <section className="panel table-wrap"><table><thead><tr><th>游戏</th><th>决定</th><th>原因</th><th>时间</th></tr></thead><tbody>{history.items.map((item) => <tr key={item.reviewEventId}><td><strong>{item.title}</strong><details className="technical-details"><summary>技术详情</summary><code>{item.importItemId}<br />{item.reviewEventId}</code></details></td><td><StatusBadge tone={item.decision === "APPROVED" ? "good" : "bad"}>{item.decision === "APPROVED" ? "已发布" : "已丢弃"}</StatusBadge></td><td>{item.reason ?? "—"}</td><td>{formatTime(item.createdAtMs)}</td></tr>)}</tbody></table></section>}</>;
}
