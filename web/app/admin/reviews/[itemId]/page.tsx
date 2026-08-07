import { ButtonLink, PageHeader, StatusBadge } from "@/components/ui";
import { FlashToast } from "@/components/flash-toast";
import { ReviewActions, type ReviewWorkspace } from "@/features/reviews/review-actions";
import { type ReviewQueueItem } from "@/features/reviews/review-queue";
import { backendJSON, formatBytes, type ListResponse } from "@/lib/backend";
import { statusTone } from "@/lib/status";
import Link from "next/link";

type DependencySnapshot = {
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
  sourceFiles: Array<{ uploadFileId: string; name: string; sizeBytes: number; sha256: string; md5: string; crc32: string; archive: boolean; archiveEntries: Array<{ name: string; sizeBytes: number; crc32: string }> }>;
  validation: (NonNullable<ReviewWorkspace["validation"]> & { dependencySnapshot?: DependencySnapshot }) | null;
};

const validationLabels: Record<string, string> = { READY: "可以发布", BLOCKED: "缺少依赖", DEPENDENCY_MISSING: "缺少依赖", INCOMPATIBLE: "不兼容", NEEDS_VALIDATION: "等待检查", PENDING: "等待检查" };
const roleLabels: Record<string, string> = { CONTENT: "游戏文件", DOS_SOURCE: "DOS 游戏文件", COMPANION: "配套文件" };

function DependencySummary({ snapshot }: { snapshot: DependencySnapshot }) {
  if (!snapshot.machine) return <div className="dependency-summary"><div className="dependency-item"><strong>运行文件已就绪</strong><span>当前平台无需额外的街机父集或 BIOS 检查</span></div><div className="metric-line"><span>检查结果</span><strong>通过</strong></div></div>;
  const issues = [
    ...(snapshot.missingEntries ?? []).map((entry) => `缺失：${entry}`),
    ...(snapshot.mismatchedEntries ?? []).map((entry) => `不匹配：${entry}`),
    ...(snapshot.warnings ?? []).map((entry) => `警告：${entry}`),
  ];
  return <section className="dependency-summary" aria-label="不可变依赖快照摘要">
    <div className="dependency-primary"><span>识别到的街机条目</span><strong>{snapshot.machine}</strong></div>
    <div className="dependency-facts"><div><span>游戏文件</span><strong>{snapshot.closure?.length ?? 0}</strong></div><div><span>外部依赖</span><strong>{snapshot.dependencies?.length ?? 0}</strong></div><div><span>异常项</span><strong>{issues.length}</strong></div></div>
    {(snapshot.dependencies ?? []).map((dependency, index) => <div className="dependency-item" key={`${dependency.kind}-${dependency.machine}-${index}`}>
      <strong>{dependency.machine ?? "未知依赖"}</strong>
      <span>{dependency.state === "MISSING" ? "缺少文件" : dependency.state === "SATISFIED_EXTERNAL" ? "已满足" : "依赖检查"} · {dependency.requiredEntries?.length ?? 0} 个文件</span>
    </div>)}
    {issues.length ? <ul className="dependency-issues">{issues.map((issue) => <li key={issue}>{issue}</li>)}</ul> : <p className="status good">游戏文件和运行依赖均已就绪</p>}
  </section>;
}

function GameFiles({ review }: { review: Review }) {
  if (review.sourceFiles?.length) return <div className="review-source-packages">{review.sourceFiles.map((file) => <details className="review-source-package" open={file.archive} key={file.uploadFileId}><summary><span>{file.archive ? "压缩包" : "游戏文件"}</span><strong title={file.name}>{file.name}</strong><small title={`${formatBytes(file.sizeBytes)} / SHA-256 ${file.sha256}`}>{formatBytes(file.sizeBytes)} / {file.sha256.slice(0, 12)}…</small></summary>{file.archive ? <div className="review-archive-entries" aria-label={`${file.name} 文件列表`}>{file.archiveEntries.length ? file.archiveEntries.map((entry, index) => <div key={`${entry.name}-${index}`}><strong title={entry.name}>{entry.name}</strong><span>{formatBytes(entry.sizeBytes)}</span><code title={`CRC32 ${entry.crc32}`}>{entry.crc32}</code></div>) : <p>压缩包内没有可展示的文件记录。</p>}</div> : null}</details>)}</div>;
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
    backendJSON<ListResponse<ReviewQueueItem>>(`/api/v1/admin/reviews${listQuery}${listQuery ? "&" : "?"}limit=100`),
  ]);
  const validationStatus = review.validation?.status ?? "PENDING";
  const selectedIndex = context.items.findIndex((item) => item.itemId === itemId);
  const nextItem = context.items[selectedIndex + 1] ?? context.items[selectedIndex - 1] ?? null;
  return <><FlashToast />
    <PageHeader title="审核条目" description="核对游戏文件、运行检查和游戏信息，确认无误后发布。" actions={<ButtonLink href={returnTo} secondary>返回待审核列表</ButtonLink>} />
    <div className="review-detail-workbench">
      <aside className="panel review-context-queue"><div className="panel-head"><div><h2>当前队列</h2><p>{context.items.length} 条 · 可任意选择</p></div></div><nav aria-label="当前待审队列">{context.items.map((item) => <Link className={item.itemId === itemId ? "review-context-item is-active" : "review-context-item"} href={`/admin/reviews/${item.itemId}?returnTo=${encodeURIComponent(returnTo)}`} key={item.itemId}><strong>{item.draftTitle}</strong><small>{item.sourceDisplayName} · {validationLabels[item.validationStatus] ?? item.validationStatus}</small></Link>)}</nav></aside>
      <ReviewActions review={review} returnTo={returnTo} nextItemId={nextItem?.itemId ?? null}>
        <div className="review-overview-grid">
          <section className="review-overview-card"><div className="review-overview-title"><div><h3>游戏文件</h3><p>目标目录：{review.platformInstance.name}</p></div><StatusBadge tone="info">{review.sourceFiles?.length ?? review.sourceManifest.files.length} 个</StatusBadge></div><GameFiles review={review} /></section>
          <section className="review-overview-card"><div className="review-overview-title"><div><h3>运行检查</h3><p>发布前必须处于可以运行状态</p></div><StatusBadge tone={statusTone(review.validation?.compatibilityCode ?? validationStatus)}>{validationLabels[review.validation?.compatibilityCode ?? validationStatus] ?? "需要检查"}</StatusBadge></div>{review.validation?.dependencySnapshot ? <DependencySummary snapshot={review.validation.dependencySnapshot} /> : <p className="muted-copy">运行检查尚未完成，暂时不能发布。</p>}</section>
        </div>
      </ReviewActions>
    </div>
  </>;
}
