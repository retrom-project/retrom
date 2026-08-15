import { ListFilters } from "@/components/list-filters";
import { ButtonLink, EmptyState, PageHeader } from "@/components/ui";
import { scalarSearchParams, withQuery, type ListResponse } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";
import { ReviewQueue, type ReviewQueueItem } from "@/features/reviews/review-queue";
import { FlashToast } from "@/components/flash-toast";
import { loadActiveTags } from "@/features/tags/tag-library";

export const metadata = { title: "待审核" };

export default async function ReviewsPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const values = scalarSearchParams(await searchParams, ["q", "tagId", "importJobId", "pegasusImportId", "platformInstanceId", "blockerCode", "sort"]);
  const [queue, activeTags] = await Promise.all([
    backendJSON<ListResponse<ReviewQueueItem>>(withQuery("/api/v1/admin/reviews", { ...values, limit: "20" })),
    loadActiveTags(),
  ]);
  const queueKey = new URLSearchParams(values).toString();
  const pegasusImportId = values.pegasusImportId;
  return <div className="import-workflow-page review-workflow-page"><FlashToast /><PageHeader eyebrow={pegasusImportId ? "Pegasus 导入 / 逐项审核" : "游戏入库"} title={pegasusImportId ? "审核这批 Pegasus 游戏" : "待审核"} description={pegasusImportId ? "这批内容不会自动发布。请逐条核对运行检查、游戏信息和媒体后再作决定。" : "核对运行检查和游戏信息，优先处理等待时间长、运行异常或信息不完整的条目。"} actions={<>{pegasusImportId ? <ButtonLink href={`/admin/imports/server/pegasus/${pegasusImportId}`} secondary>← 返回 Pegasus 任务</ButtonLink> : null}<ButtonLink href="/admin/reviews/history" secondary>审核历史</ButtonLink></>} /><ListFilters action="/admin/reviews" placeholder="输入游戏标题、来源文件或标签" values={values} fixedFilters={pegasusImportId ? [{ name: "pegasusImportId", value: pegasusImportId }] : []} preserveFixedFiltersOnReset={Boolean(pegasusImportId)} textFilters={pegasusImportId ? [] : [{ name: "importJobId", label: "导入批次", placeholder: "输入完整批次编号" }]} filters={[{ name: "tagId", label: "标签", options: [{ value: "", label: "所有标签" }, ...activeTags.map((tag) => ({ value: tag.tagId, label: tag.name }))] }, { name: "blockerCode", label: "运行检查", options: [{ value: "", label: "所有状态" }, { value: "READY", label: "可以发布" }, { value: "DEPENDENCY_MISSING", label: "缺少所需文件" }, { value: "INCOMPATIBLE", label: "当前不兼容" }] }, { name: "sort", label: "排列顺序", options: [{ value: "", label: "等待最久的优先" }, { value: "UPDATED_DESC", label: "最近更新的优先" }] }]} />{queue.items.length === 0 ? <EmptyState title={pegasusImportId ? "这批游戏已经全部处理" : "待审核队列已清空"} description={pegasusImportId ? "没有剩余待审核条目；可以返回 Pegasus 任务查看已发布、已丢弃和异常结果。" : "新导入的游戏完成识别和运行检查后，会进入这里。"} /> : <ReviewQueue key={queueKey} initial={queue} values={values} />}</div>;
}
