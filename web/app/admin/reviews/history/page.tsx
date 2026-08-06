import { ListFilters } from "@/components/list-filters";
import { EmptyState, PageHeader, StatusBadge } from "@/components/ui";
import { backendJSON, formatTime, scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";

type HistoryItem = { reviewEventId: string; importItemId: string; importJobId: string; title: string; decision: "APPROVED" | "DISCARDED"; reason: string | null; createdAtMs: number };

export const metadata = { title: "审核历史" };

export default async function ReviewHistoryPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "decision"]);
  const history = await backendJSON<ListResponse<HistoryItem>>(withQuery("/api/v1/admin/review-history", values));
  return <><PageHeader title="审核历史" description="只读回放最终发布或丢弃决定，以及当时锁定的输入与配置快照。" /><ListFilters action="/admin/reviews/history" placeholder="搜索标题或来源…" values={values} filters={[{ name: "decision", label: "审核决定", options: [{ value: "", label: "所有决定" }, { value: "APPROVED", label: "已发布" }, { value: "DISCARDED", label: "已丢弃" }] }]} />{history.items.length === 0 ? <EmptyState title="尚无审核历史" description="通过或丢弃条目后，不可变 ReviewEvent 会显示在这里。" /> : <section className="panel table-wrap"><table><thead><tr><th>游戏 / 条目</th><th>决定</th><th>原因</th><th>时间</th><th>证据 ID</th></tr></thead><tbody>{history.items.map((item) => <tr key={item.reviewEventId}><td><strong>{item.title}</strong><small>{item.importItemId.slice(0, 12)}…</small></td><td><StatusBadge tone={item.decision === "APPROVED" ? "good" : "bad"}>{item.decision === "APPROVED" ? "已发布" : "已丢弃"}</StatusBadge></td><td>{item.reason ?? "—"}</td><td>{formatTime(item.createdAtMs)}</td><td><span className="row-action" title={item.reviewEventId}>{item.reviewEventId.slice(0, 12)}…</span></td></tr>)}</tbody></table></section>}</>;
}
