import { ListFilters } from "@/components/list-filters";
import { ButtonLink, EmptyState, PageHeader } from "@/components/ui";
import { backendJSON, scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";
import { ReviewHistory, type HistoryItem } from "@/features/reviews/review-history";

export const metadata = { title: "审核历史" };

export default async function ReviewHistoryPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "decision"]);
  const history = await backendJSON<ListResponse<HistoryItem>>(withQuery("/api/v1/admin/review-history", values));
  const approved = history.items.filter((item) => item.decision === "APPROVED").length;
  const discarded = history.items.length - approved;
  return <div className="import-workflow-page review-history-page"><PageHeader eyebrow="游戏入库" title="审核历史" description="查询已经发布或丢弃的条目，并查看审核完成时采用的信息和决定。" actions={<ButtonLink href="/admin/reviews" secondary>返回待审核</ButtonLink>} /><ListFilters action="/admin/reviews/history" placeholder="输入游戏标题或来源文件" values={values} filters={[{ name: "decision", label: "处理结果", options: [{ value: "", label: "全部结果" }, { value: "APPROVED", label: "已发布" }, { value: "DISCARDED", label: "已丢弃" }] }]} /><div className="import-workflow-chips"><span className="is-active">当前已加载 {history.items.length}</span><span>已发布 {approved}</span><span>已丢弃 {discarded}</span></div>{history.items.length === 0 ? <EmptyState title="尚无审核历史" description="通过或丢弃条目后，记录会显示在这里。" /> : <ReviewHistory items={history.items} />}</div>;
}
