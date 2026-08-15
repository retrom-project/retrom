import { ListFilters } from "@/components/list-filters";
import { ButtonLink, EmptyState, PageHeader } from "@/components/ui";
import { scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";
import { ReviewQueue, ReviewQueueRecovery, type ReviewQueueItem } from "@/features/reviews/review-queue";
import { FlashToast } from "@/components/flash-toast";
import { loadActiveTags } from "@/features/tags/tag-library";
import { ReviewBulkApproval } from "@/features/reviews/review-bulk-approval";

export const metadata = { title: "待审核" };

export default async function ReviewsPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const parameters = await searchParams;
  const values = scalarSearchParams(parameters, ["q", "tagId", "importJobId", "pegasusImportId", "platformInstanceId", "blockerCode", "sort"]);
  const staleReview = parameters.reviewNotice === "stale";
  const [queue, activeTags] = await Promise.all([
    backendJSON<ListResponse<ReviewQueueItem>>(withQuery("/api/v1/admin/reviews", { ...values, limit: "20" })),
    loadActiveTags(),
  ]);
  const queueKey = new URLSearchParams(values).toString();
  const pegasusImportId = values.pegasusImportId;
  const restoreBulkApprovalId = typeof parameters.bulkApprovalId === "string" ? parameters.bulkApprovalId : undefined;
  return <div className="import-workflow-page review-workflow-page"><FlashToast /><ReviewQueueRecovery active={staleReview} values={values} /><PageHeader eyebrow={pegasusImportId ? "Pegasus 导入 / 快速审批" : "游戏入库"} title={pegasusImportId ? "审核这批 Pegasus 游戏" : "待审核"} description={pegasusImportId ? "严格检查通过的游戏可以快速发布；截图放行、重复内容和运行问题仍需逐项处理。" : "核对运行检查和游戏信息，或快速发布当前筛选范围内严格检查通过的游戏。"} actions={<>{pegasusImportId ? <ButtonLink href={`/admin/imports/server/pegasus/${pegasusImportId}`} secondary>← 返回 Pegasus 任务</ButtonLink> : null}<ReviewBulkApproval values={values} restoreBulkApprovalId={restoreBulkApprovalId} /><ButtonLink href="/admin/reviews/history" secondary>审核历史</ButtonLink></>} /><div id="review-bulk-status-root" /><ListFilters action="/admin/reviews" placeholder="输入游戏标题、来源文件或标签" values={values} fixedFilters={pegasusImportId ? [{ name: "pegasusImportId", value: pegasusImportId }] : []} preserveFixedFiltersOnReset={Boolean(pegasusImportId)} textFilters={pegasusImportId ? [] : [{ name: "importJobId", label: "导入批次", placeholder: "输入完整批次编号" }]} filters={[{ name: "tagId", label: "标签", options: [{ value: "", label: "所有标签" }, ...activeTags.map((tag) => ({ value: tag.tagId, label: tag.name }))] }, { name: "blockerCode", label: "运行检查", options: [{ value: "", label: "所有状态" }, { value: "READY", label: "可以发布" }, { value: "DEPENDENCY_MISSING", label: "缺少所需文件" }, { value: "INCOMPATIBLE", label: "当前不兼容" }] }, { name: "sort", label: "排列顺序", options: [{ value: "", label: "等待最久的优先" }, { value: "UPDATED_DESC", label: "最近更新的优先" }] }]} />{queue.items.length === 0 ? <EmptyState title={pegasusImportId ? "这批游戏已经全部处理" : "待审核队列已清空"} description={pegasusImportId ? "没有剩余待审核条目；可以返回 Pegasus 任务查看已发布、已丢弃和异常结果。" : "新导入的游戏完成识别和运行检查后，会进入这里。"} /> : <ReviewQueue key={queueKey} initial={queue} values={values} resetPersisted={staleReview} />}</div>;
}
