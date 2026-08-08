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
  const ready = queue.items.filter((item) => item.validationStatus === "READY" && item.blockerCodes.length === 0).length;
  const abnormal = queue.items.filter((item) => item.validationStatus !== "READY" || item.blockerCodes.length > 0).length;
  const missing = queue.items.filter((item) => item.candidateCount === 0).length;
  return <div className="import-workflow-page review-workflow-page"><FlashToast /><PageHeader eyebrow="游戏入库" title="待审核" description="核对运行检查和游戏信息，优先处理等待时间长、运行异常或信息不完整的条目。" actions={<ButtonLink href="/admin/reviews/history" secondary>审核历史</ButtonLink>} /><ListFilters action="/admin/reviews" placeholder="输入游戏标题或来源文件" values={values} textFilters={[{ name: "importJobId", label: "导入批次", placeholder: "输入完整批次编号" }]} filters={[{ name: "blockerCode", label: "运行检查", options: [{ value: "", label: "所有状态" }, { value: "READY", label: "可以发布" }, { value: "DEPENDENCY_MISSING", label: "缺少所需文件" }, { value: "INCOMPATIBLE", label: "当前不兼容" }] }, { name: "sort", label: "排列顺序", options: [{ value: "", label: "等待最久的优先" }, { value: "UPDATED_DESC", label: "最近更新的优先" }] }]} /><div className="import-workflow-chips review-workflow-chips"><span className="is-active">当前已加载 {queue.items.length}</span><span>可以发布 {ready}</span><span>运行异常 {abnormal}</span><span>未找到信息 {missing}</span></div>{queue.items.length === 0 ? <EmptyState title="待审核队列已清空" description="新导入的游戏完成识别和运行检查后，会进入这里。" /> : <ReviewQueue key={queueKey} initial={queue} values={values} />}</div>;
}
