import { ListFilters } from "@/components/list-filters";
import { ButtonLink, EmptyState, PageHeader } from "@/components/ui";
import { backendJSON, scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";
import { ReviewQueue, type ReviewQueueItem } from "@/features/reviews/review-queue";
import { FlashToast } from "@/components/flash-toast";

export const metadata = { title: "待审核" };

export default async function ReviewsPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "importJobId", "platformInstanceId", "blockerCode", "sort"]);
  const queue = await backendJSON<ListResponse<ReviewQueueItem>>(withQuery("/api/v1/admin/reviews", { ...values, limit: "50" }));
  const queueKey = new URLSearchParams(values).toString();
  return <><FlashToast /><PageHeader title="待审核" description="核对游戏文件、兼容性和游戏信息，确认无误后发布到资料库。" actions={<ButtonLink href="/admin/reviews/history" secondary>审核历史</ButtonLink>} /><ListFilters action="/admin/reviews" placeholder="搜索来源文件或草稿标题…" values={values} textFilters={[{ name: "importJobId", label: "导入批次编号", placeholder: "输入完整批次编号" }]} filters={[{ name: "blockerCode", label: "兼容性状态", options: [{ value: "", label: "所有状态" }, { value: "READY", label: "可发布" }, { value: "DEPENDENCY_MISSING", label: "缺少依赖" }, { value: "INCOMPATIBLE", label: "不兼容" }] }, { name: "sort", label: "更新时间排序", options: [{ value: "", label: "最早待审优先" }, { value: "UPDATED_DESC", label: "最近更新优先" }] }]} />{queue.items.length === 0 ? <EmptyState title="待审核队列已清空" description="新导入的游戏完成识别和兼容性检查后，会进入这里。" /> : <ReviewQueue key={queueKey} initial={queue} values={values} />}</>;
}
