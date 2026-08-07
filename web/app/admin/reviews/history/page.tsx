import { ListFilters } from "@/components/list-filters";
import { EmptyState, PageHeader } from "@/components/ui";
import { backendJSON, scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";
import { ReviewHistory, type HistoryItem } from "@/features/reviews/review-history";

export const metadata = { title: "审核历史" };

export default async function ReviewHistoryPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "decision"]);
  const history = await backendJSON<ListResponse<HistoryItem>>(withQuery("/api/v1/admin/review-history", values));
  return <><PageHeader title="审核历史" description="查看已经发布或丢弃的游戏，以及当时采用的信息和决定。" /><ListFilters action="/admin/reviews/history" placeholder="输入游戏标题或来源" values={values} resultCount={history.items.length} filters={[{ name: "decision", label: "处理结果", options: [{ value: "", label: "全部结果" }, { value: "APPROVED", label: "已发布" }, { value: "DISCARDED", label: "已丢弃" }] }]} />{history.items.length === 0 ? <EmptyState title="尚无审核历史" description="通过或丢弃条目后，记录会显示在这里。" /> : <ReviewHistory items={history.items} />}</>;
}
