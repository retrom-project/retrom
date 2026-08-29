import { ButtonLink, PageHeader, StatusBadge } from "@/components/ui";
import { FlashToast } from "@/components/flash-toast";
import { ReviewActions, type ReviewWorkspace } from "@/features/reviews/review-actions";
import { adjacentReviewItemId } from "@/features/reviews/review-navigation";
import { type ReviewQueueItem } from "@/features/reviews/review-queue";
import { ReviewValidationGuidance, reviewCompatibilityLabel, type ReviewDependencySnapshot } from "@/features/reviews/review-validation-guidance";
import { formatBytes, type ListResponse } from "@/lib/backend";
import { BackendResponseError, backendJSON } from "@/lib/server-backend";
import { statusTone } from "@/lib/status";
import { loadActiveTags } from "@/features/tags/tag-library";
import { boundedReviewSourcePreview, reviewWorkspaceWithoutSourceEvidence } from "@/features/reviews/review-source-preview";
import { redirect } from "next/navigation";

type DependencySnapshot = ReviewDependencySnapshot & {
  machine?: string;
  datVersionId?: string;
  closure?: string[];
  dependencies?: Array<{ kind?: string; machine?: string; state?: string; requiredEntries?: string[] }>;
  missingEntries?: string[];
  mismatchedEntries?: string[];
  warnings?: string[];
};

type Review = ReviewWorkspace & {
  importJobId: string;
  platformInstance: { id: string; name: string };
  sourceManifest: { files: Array<{ logicalName: string; role: string; sizeBytes?: number; blobSha256?: string; sourceArchiveSha256?: string | null }> };
  sourceFiles: Array<{ uploadFileId: string; name: string; sizeBytes: number; sha256: string; md5: string; crc32: string; archive: boolean; archiveFormat: "ZIP" | "SEVEN_Z" | null; archiveEntries: Array<{ name: string; sizeBytes: number; crc32: string }> }>;
  validation: (NonNullable<ReviewWorkspace["validation"]> & { dependencySnapshot?: DependencySnapshot }) | null;
};

const roleLabels: Record<string, string> = { CONTENT: "游戏文件", DOS_SOURCE: "DOS 游戏文件", COMPANION: "配套文件" };

function GameFiles({ review }: { review: Review }) {
  if (review.sourceFiles?.length) {
    const files = boundedReviewSourcePreview(review.sourceFiles);
    return <div className="review-source-packages">{files.visible.map((file) => <SourcePackage file={file} key={file.uploadFileId} />)}<PreviewNotice hidden={files.hidden} total={files.total} /></div>;
  }
  const files = boundedReviewSourcePreview(review.sourceManifest.files);
  return <div className="review-file-list">{files.visible.map((file, index) => <div key={`${file.role}-${file.logicalName}-${index}`}><span>{roleLabels[file.role] ?? "文件"}</span><strong title={file.logicalName}>{file.logicalName}</strong><small>{file.sizeBytes === undefined ? "大小未知" : formatBytes(file.sizeBytes)}</small></div>)}<PreviewNotice hidden={files.hidden} total={files.total} /></div>;
}

function SourcePackage({ file }: { file: Review["sourceFiles"][number] }) {
  const entries = boundedReviewSourcePreview(file.archiveEntries);
  return <details className="review-source-package" open={file.archive}><summary><span>{file.archive ? `${file.archiveFormat === "SEVEN_Z" ? "7z" : "ZIP"} 来源包` : "游戏文件"}</span><strong title={file.name}>{file.name}</strong><small title={`${formatBytes(file.sizeBytes)} / SHA-256 ${file.sha256}`}>{formatBytes(file.sizeBytes)} / {file.sha256.slice(0, 12)}…</small></summary>{file.archive ? <div className="review-archive-entries" aria-label={`${file.name} 文件列表`}><p>运行时使用全部成员物化后的原始内容；这里仅展示有界审计预览，来源包保留为证据。</p>{entries.visible.length ? entries.visible.map((entry, index) => <div key={`${entry.name}-${index}`}><strong title={entry.name}>{entry.name}</strong><span>{formatBytes(entry.sizeBytes)}</span><code title={`CRC32 ${entry.crc32}`}>{entry.crc32}</code></div>) : <p>压缩包内没有可展示的文件记录。</p>}<PreviewNotice hidden={entries.hidden} total={entries.total} /></div> : null}</details>;
}

function PreviewNotice({ hidden, total }: { hidden: number; total: number }) {
  return hidden > 0 ? <p className="muted">仅展示前 {total - hidden} 项，共 {total} 项；完整清单仍参与内容摘要和发布。</p> : null;
}

function safeReturnTo(raw: string | undefined) {
  if (!raw) {return "/admin/reviews";}
  const parsed = new URL(raw, "http://retrom.invalid");
  const allowed = new Set(["q", "tagId", "importJobId", "pegasusImportId", "emulationStationImportId", "platformInstanceId", "blockerCode", "sort"]);
  if (parsed.origin !== "http://retrom.invalid" || parsed.pathname !== "/admin/reviews") {return "/admin/reviews";}
  for (const [name] of parsed.searchParams) {if (!allowed.has(name) || parsed.searchParams.getAll(name).length !== 1) {return "/admin/reviews";}}
  return `${parsed.pathname}${parsed.search}`;
}

async function loadPendingReview(itemId: string) {
  try {
    return await backendJSON<Review>(`/api/v1/admin/reviews/${itemId}`);
  } catch (error) {
    if (error instanceof BackendResponseError && error.status === 404) {return null;}
    throw error;
  }
}

function staleReviewReturnTo(returnTo: string) {
  const parsed = new URL(returnTo, "http://retrom.invalid");
  parsed.searchParams.set("reviewNotice", "stale");
  return `${parsed.pathname}${parsed.search}`;
}

type ReviewDetailSummary = {
  compatibilityCode: string;
  compatibilityLabel: string;
  dependencyCount: number;
  dependencyIssueCount: number;
  dependencySnapshot: DependencySnapshot | undefined;
  sourceDisplayName: string;
  validationStatus: string;
};

function missingRequiredBIOSCount(snapshot: DependencySnapshot | undefined) {
  return (snapshot?.bios ?? [])
    .filter((item) => item.requirementMode !== "OPTIONAL" && !item.blobId).length;
}

function dependencyIssueCount(snapshot: DependencySnapshot | undefined) {
  return (snapshot?.missingEntries?.length ?? 0)
    + (snapshot?.mismatchedEntries?.length ?? 0)
    + (snapshot?.warnings?.length ?? 0)
    + missingRequiredBIOSCount(snapshot);
}

function dependencyCount(snapshot: DependencySnapshot | undefined) {
  return (snapshot?.dependencies?.length ?? 0) + (snapshot?.bios?.length ?? 0);
}

function reviewSourceDisplayName(review: Review) {
  return review.sourceFiles?.[0]?.name
    ?? review.sourceManifest.files[0]?.logicalName
    ?? "游戏文件";
}

function summarizeReview(review: Review): ReviewDetailSummary {
  const validationStatus = review.validation?.status ?? "PENDING";
  const compatibilityCode = review.validation?.compatibilityCode ?? validationStatus;
  const dependencySnapshot = review.validation?.dependencySnapshot;
  return {
    compatibilityCode,
    compatibilityLabel: reviewCompatibilityLabel(compatibilityCode, validationStatus),
    dependencyCount: dependencyCount(dependencySnapshot),
    dependencyIssueCount: dependencyIssueCount(dependencySnapshot),
    dependencySnapshot,
    sourceDisplayName: reviewSourceDisplayName(review),
    validationStatus,
  };
}

function ReviewCapability({ review, summary }: { review: Review; summary: ReviewDetailSummary }) {
  const sourceSize = review.sourceFiles?.[0]
    ? formatBytes(review.sourceFiles[0].sizeBytes)
    : `${review.sourceManifest.files.length} 个来源文件`;
  const runtimeCheck = summary.dependencySnapshot?.machine
    ? `识别为 ${summary.dependencySnapshot.machine} · ${summary.compatibilityLabel}`
    : summary.compatibilityLabel;
  const dependencyCheck = summary.dependencySnapshot
    ? `${summary.dependencyCount} 项运行依赖 · ${summary.dependencyIssueCount ? `${summary.dependencyIssueCount} 项需要处理` : "没有发现异常"}`
    : "检查结果尚未生成";
  return <section className="panel review-workflow-capability">
    <div className="panel-head"><div><h2>① 能不能发布？</h2><p>直接展示文件、运行方式和依赖检查结论。</p></div><StatusBadge tone={statusTone(summary.compatibilityCode)}>{summary.compatibilityLabel}</StatusBadge></div>
    <div className="panel-body review-capability-list">
      <ReviewValidationGuidance status={summary.validationStatus} compatibilityCode={summary.compatibilityCode} snapshot={summary.dependencySnapshot} />
      <div><strong>游戏文件</strong><span>{summary.sourceDisplayName} · {sourceSize}</span></div>
      <div><strong>运行检查</strong><span>{runtimeCheck}</span></div>
      <div><strong>依赖检查</strong><span>{dependencyCheck}</span></div>
    </div>
  </section>;
}

function ReviewDetail({ activeTags, nextItemId, returnTo, review }: {
  activeTags: Awaited<ReturnType<typeof loadActiveTags>>;
  nextItemId: string | null;
  returnTo: string;
  review: Review;
}) {
  const summary = summarizeReview(review);
  const clientReview = reviewWorkspaceWithoutSourceEvidence(review);
  return <div className="import-workflow-page review-detail-prototype"><FlashToast />
    <PageHeader eyebrow="待审核 / 条目" title="审核条目" description="先判断能不能发布，再确认发布成什么。技术证据按需展开，不挤占主决策。" actions={<ButtonLink href={returnTo} secondary>← 返回待审核列表</ButtonLink>} />
    <ReviewActions review={clientReview} activeTags={activeTags} returnTo={returnTo} nextItemId={nextItemId} sourceDisplayName={summary.sourceDisplayName} platformInstanceName={review.platformInstance.name}>
      <ReviewCapability review={review} summary={summary} />
      <section className="panel review-workflow-files"><div className="panel-head"><div><h2>来源文件</h2><p>用于核对这条游戏来自哪份内容。</p></div></div><div className="panel-body"><GameFiles review={review} /></div></section>
    </ReviewActions>
  </div>;
}

export default async function ReviewDetailPage({ params, searchParams }: { params: Promise<{ itemId: string }>; searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const { itemId } = await params;
  const queryParameters = await searchParams;
  const returnTo = safeReturnTo(typeof queryParameters.returnTo === "string" ? queryParameters.returnTo : undefined);
  const listQuery = new URL(returnTo, "http://retrom.invalid").search;
  const [review, context, activeTags] = await Promise.all([
    loadPendingReview(itemId),
    backendJSON<ListResponse<ReviewQueueItem>>(`/api/v1/admin/reviews${listQuery}${listQuery ? "&" : "?"}limit=20`),
    loadActiveTags(),
  ]);
  if (!review) {redirect(staleReviewReturnTo(returnTo));}
  const nextItemId = adjacentReviewItemId(context.items.map((item) => item.itemId), itemId);
  return <ReviewDetail activeTags={activeTags} nextItemId={nextItemId} returnTo={returnTo} review={review} />;
}
