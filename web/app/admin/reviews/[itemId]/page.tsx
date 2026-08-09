import { ButtonLink, PageHeader, StatusBadge } from "@/components/ui";
import { FlashToast } from "@/components/flash-toast";
import { ReviewActions, type ReviewWorkspace } from "@/features/reviews/review-actions";
import { type ReviewQueueItem } from "@/features/reviews/review-queue";
import { ReviewValidationGuidance, reviewCompatibilityLabel, type ReviewDependencySnapshot } from "@/features/reviews/review-validation-guidance";
import { formatBytes, type ListResponse } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";
import { statusTone } from "@/lib/status";

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
  if (review.sourceFiles?.length) return <div className="review-source-packages">{review.sourceFiles.map((file) => <details className="review-source-package" open={file.archive} key={file.uploadFileId}><summary><span>{file.archive ? `${file.archiveFormat === "SEVEN_Z" ? "7z" : "ZIP"} 来源包` : "游戏文件"}</span><strong title={file.name}>{file.name}</strong><small title={`${formatBytes(file.sizeBytes)} / SHA-256 ${file.sha256}`}>{formatBytes(file.sizeBytes)} / {file.sha256.slice(0, 12)}…</small></summary>{file.archive ? <div className="review-archive-entries" aria-label={`${file.name} 文件列表`}><p>运行时使用下列唯一匹配成员物化后的原始内容，来源包仅保留为证据。</p>{file.archiveEntries.length ? file.archiveEntries.map((entry, index) => <div key={`${entry.name}-${index}`}><strong title={entry.name}>{entry.name}</strong><span>{formatBytes(entry.sizeBytes)}</span><code title={`CRC32 ${entry.crc32}`}>{entry.crc32}</code></div>) : <p>压缩包内没有可展示的文件记录。</p>}</div> : null}</details>)}</div>;
  return <div className="review-file-list">{review.sourceManifest.files.map((file, index) => <div key={`${file.role}-${file.logicalName}-${index}`}><span>{roleLabels[file.role] ?? "文件"}</span><strong title={file.logicalName}>{file.logicalName}</strong><small>{file.sizeBytes === undefined ? "大小未知" : formatBytes(file.sizeBytes)}</small></div>)}</div>;
}

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
    backendJSON<ListResponse<ReviewQueueItem>>(`/api/v1/admin/reviews${listQuery}${listQuery ? "&" : "?"}limit=20`),
  ]);
  const validationStatus = review.validation?.status ?? "PENDING";
  const selectedIndex = context.items.findIndex((item) => item.itemId === itemId);
  const nextItem = selectedIndex < 0 ? null : context.items[selectedIndex + 1] ?? context.items[selectedIndex - 1] ?? null;
  const sourceDisplayName = review.sourceFiles?.[0]?.name ?? review.sourceManifest.files[0]?.logicalName ?? "游戏文件";
  const compatibilityCode = review.validation?.compatibilityCode ?? validationStatus;
  const compatibilityLabel = reviewCompatibilityLabel(compatibilityCode, validationStatus);
  const dependencySnapshot = review.validation?.dependencySnapshot;
  const missingRequiredBIOSCount = (dependencySnapshot?.bios ?? []).filter((item) => item.requirementMode !== "OPTIONAL" && !item.blobId).length;
  const dependencyIssueCount = (dependencySnapshot?.missingEntries?.length ?? 0)
    + (dependencySnapshot?.mismatchedEntries?.length ?? 0)
    + (dependencySnapshot?.warnings?.length ?? 0)
    + missingRequiredBIOSCount;
  const dependencyCount = (dependencySnapshot?.dependencies?.length ?? 0) + (dependencySnapshot?.bios?.length ?? 0);
  return <div className="import-workflow-page review-detail-prototype"><FlashToast />
    <PageHeader eyebrow="待审核 / 条目" title="审核条目" description="先判断能不能发布，再确认发布成什么。技术证据按需展开，不挤占主决策。" actions={<ButtonLink href={returnTo} secondary>← 返回待审核列表</ButtonLink>} />
    <ReviewActions review={review} returnTo={returnTo} nextItemId={nextItem?.itemId ?? null} sourceDisplayName={sourceDisplayName} platformInstanceName={review.platformInstance.name}>
      <section className="panel review-workflow-capability"><div className="panel-head"><div><h2>① 能不能发布？</h2><p>直接展示文件、运行方式和依赖检查结论。</p></div><StatusBadge tone={statusTone(compatibilityCode)}>{compatibilityLabel}</StatusBadge></div><div className="panel-body review-capability-list"><ReviewValidationGuidance status={validationStatus} compatibilityCode={compatibilityCode} snapshot={dependencySnapshot} /><div><strong>游戏文件</strong><span>{sourceDisplayName} · {review.sourceFiles?.[0] ? formatBytes(review.sourceFiles[0].sizeBytes) : `${review.sourceManifest.files.length} 个来源文件`}</span></div><div><strong>运行检查</strong><span>{dependencySnapshot?.machine ? `识别为 ${dependencySnapshot.machine} · ${compatibilityLabel}` : compatibilityLabel}</span></div><div><strong>依赖检查</strong><span>{dependencySnapshot ? `${dependencyCount} 项运行依赖 · ${dependencyIssueCount ? `${dependencyIssueCount} 项需要处理` : "没有发现异常"}` : "检查结果尚未生成"}</span></div></div></section>
      <section className="panel review-workflow-files"><div className="panel-head"><div><h2>来源文件</h2><p>用于核对这条游戏来自哪份内容。</p></div></div><div className="panel-body"><GameFiles review={review} /></div></section>
    </ReviewActions>
  </div>;
}
