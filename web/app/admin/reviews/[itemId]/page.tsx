import { ButtonLink, PageHeader, StatusBadge } from "@/components/ui";
import { ReviewActions, type ReviewWorkspace } from "@/features/reviews/review-actions";
import { type ReviewQueueItem } from "@/features/reviews/review-queue";
import { backendJSON, type ListResponse } from "@/lib/backend";
import Link from "next/link";

type Review = ReviewWorkspace & {
  importJobId: string;
  platformInstance: { id: string; name: string };
  sourceManifest: { files: Array<{ logicalName: string; role: string; sizeBytes?: number; blobSha256?: string; sourceArchiveSha256?: string | null }> };
  validation: (NonNullable<ReviewWorkspace["validation"]> & { dependencySnapshot?: unknown }) | null;
};

function safeReturnTo(raw: string | undefined) {
  if (!raw) return "/admin/reviews";
  const parsed = new URL(raw, "http://retrom.invalid");
  const allowed = new Set(["q", "importJobId", "platformInstanceId", "blockerCode", "sort"]);
  if (parsed.origin !== "http://retrom.invalid" || parsed.pathname !== "/admin/reviews") return "/admin/reviews";
  for (const [name] of parsed.searchParams) if (!allowed.has(name) || parsed.searchParams.getAll(name).length !== 1) return "/admin/reviews";
  return `${parsed.pathname}${parsed.search}`;
}

export default async function ReviewDetailPage({ params, searchParams }: { params: Promise<{ itemId: string }>; searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const { itemId } = await params;
  const queryParameters = await searchParams;
  const returnTo = safeReturnTo(typeof queryParameters.returnTo === "string" ? queryParameters.returnTo : undefined);
  const listQuery = new URL(returnTo, "http://retrom.invalid").search;
  const [review, context] = await Promise.all([
    backendJSON<Review>(`/api/v1/admin/reviews/${itemId}`),
    backendJSON<ListResponse<ReviewQueueItem>>(`/api/v1/admin/reviews${listQuery}${listQuery ? "&" : "?"}limit=100`),
  ]);
  const validationStatus = review.validation?.status ?? "PENDING";
  const selectedIndex = context.items.findIndex((item) => item.itemId === itemId);
  const nextItem = context.items[selectedIndex + 1] ?? context.items[selectedIndex - 1] ?? null;
  return <>
    <PageHeader title="审核条目" description={`逐项核对 ${itemId} 的内容证据、Core DAT 依赖与元信息来源。`} actions={<ButtonLink href={returnTo} secondary>返回待审核列表</ButtonLink>} />
    <div className="review-detail-workbench">
      <aside className="panel review-context-queue"><div className="panel-head"><div><h2>当前队列</h2><p>{context.items.length} 条 · 可非顺序选择</p></div></div><nav aria-label="当前待审队列">{context.items.map((item) => <Link className={item.itemId === itemId ? "review-context-item is-active" : "review-context-item"} href={`/admin/reviews/${item.itemId}?returnTo=${encodeURIComponent(returnTo)}`} key={item.itemId}><strong>{item.draftTitle}</strong><small>{item.sourceDisplayName} · {item.validationStatus}</small></Link>)}</nav></aside>
      <div className="stack">
        <section className="panel"><div className="panel-head"><div><h2>内容与来源证据</h2><p>文件、归档来源与 hash 在发布后仍保持可追溯</p></div><StatusBadge tone="info">{review.sourceManifest.files.length} 文件</StatusBadge></div><div className="panel-body stack"><p>目标目录：<strong>{review.platformInstance.name}</strong></p>{review.sourceManifest.files.map((file, index) => <div className="candidate" key={`${file.role}-${file.logicalName}-${index}`}><div className="metric-line"><span>{file.role}</span><strong>{file.logicalName}</strong></div>{file.sizeBytes === undefined ? null : <div className="metric-line"><span>大小</span><strong>{file.sizeBytes} bytes</strong></div>}{file.blobSha256 ? <small title={file.blobSha256}>SHA-256 {file.blobSha256.slice(0, 16)}…</small> : null}{file.sourceArchiveSha256 ? <small title={file.sourceArchiveSha256}>来源归档 {file.sourceArchiveSha256.slice(0, 16)}…</small> : null}</div>)}</div></section>
        <section className="panel"><div className="panel-head"><div><h2>Core DAT 依赖检查</h2><p>兼容性证据独立于 Hasheous 候选</p></div><StatusBadge tone={validationStatus === "READY" ? "good" : "warn"}>{validationStatus}</StatusBadge></div><div className="panel-body stack">{review.validation ? <><div className="metric-line"><span>Compatibility</span><strong>{review.validation.compatibilityCode}</strong></div><div className="metric-line"><span>Validation ID</span><strong title={review.validation.id}>{review.validation.id.slice(0, 16)}…</strong></div>{review.validation.dependencySnapshot ? <details><summary>查看不可变依赖快照</summary><pre className="evidence-json">{JSON.stringify(review.validation.dependencySnapshot, null, 2)}</pre></details> : null}</> : <p>尚无可用于当前目标目录的 Validation，发布已禁用。</p>}</div></section>
      </div>
      <section className="panel"><div className="panel-head"><div><h2>元信息、候选与发布</h2><p>候选只在明确采用并保存草稿后成为来源</p></div><StatusBadge tone={review.selectedCandidateId ? "info" : "warn"}>{review.selectedCandidateId ? "已选候选" : "人工元信息"}</StatusBadge></div><div className="panel-body"><ReviewActions review={review} returnTo={returnTo} nextItemId={nextItem?.itemId ?? null} /></div></section>
    </div>
  </>;
}
